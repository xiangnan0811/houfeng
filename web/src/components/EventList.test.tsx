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
    expect(screen.getByText('系统暂时没有新的状态变更事件。')).toBeInTheDocument()
  })

  it('renders binding audit events with sane labels and no incident-only meta row', () => {
    render(
      <EventList
        events={[
          {
            event_id: 'evt_bind_001',
            incident_id: '',
            incident_class: '',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_binding_reset',
            severity: '',
            summary: '监控实例已重置绑定并等待重新接入',
            created_at: '2026-04-26T08:10:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByText('绑定已重置')).toBeInTheDocument()
    expect(screen.getByText('监控实例')).toBeInTheDocument()
    expect(screen.getByText('mi_001')).toBeInTheDocument()
    expect(screen.getByText('监控实例已重置绑定并等待重新接入')).toBeInTheDocument()
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
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_monitoring_maintenance_entered',
            severity: '',
            summary: '监控实例已进入维护模式',
            created_at: '2026-04-26T08:10:00Z',
          },
          {
            event_id: 'evt_runtime_002',
            incident_id: '',
            incident_class: '',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_monitoring_maintenance_exited',
            severity: '',
            summary: '监控实例已退出维护模式',
            created_at: '2026-04-26T08:11:00Z',
          },
          {
            event_id: 'evt_runtime_003',
            incident_id: '',
            incident_class: '',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_monitoring_paused',
            severity: '',
            summary: '监控实例监控已暂停',
            created_at: '2026-04-26T08:12:00Z',
          },
          {
            event_id: 'evt_runtime_004',
            incident_id: '',
            incident_class: '',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_monitoring_resumed',
            severity: '',
            summary: '监控实例监控已恢复',
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

    expect(screen.getByRole('heading', { level: 3, name: '监控实例进入维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '监控实例退出维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '监控实例暂停监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '监控实例恢复监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标进入维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标退出维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已暂停' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已恢复' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已归档' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已恢复为暂停' })).toBeInTheDocument()
    expect(screen.queryByText('异常类型')).not.toBeInTheDocument()
  })

  it('renders readable lifecycle labels without incident-only meta rows', () => {
    render(
      <EventList
        events={[
          {
            event_id: 'evt_lifecycle_001',
            incident_id: '',
            incident_class: '',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_retired',
            severity: '',
            summary: '监控实例已退役并退出活跃舰队，历史记录保留',
            created_at: '2026-04-26T08:10:00Z',
          },
          {
            event_id: 'evt_lifecycle_002',
            incident_id: '',
            incident_class: '',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'monitoring_instance_restored_to_observing',
            severity: '',
            summary: '监控实例已从退役恢复到观察中',
            created_at: '2026-04-26T08:11:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByRole('heading', { level: 3, name: '监控实例已退役' })).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { level: 3, name: '监控实例恢复到观察中' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('异常类型')).not.toBeInTheDocument()
  })
})
