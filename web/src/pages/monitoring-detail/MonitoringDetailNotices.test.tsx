import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ActiveIncidentRecord, HostSample, MonitoringInstanceRecord } from '../../lib/types'
import { MonitoringDetailNotices } from './MonitoringDetailNotices'

const noop = vi.fn()

function instance(overrides: Partial<MonitoringInstanceRecord> = {}): MonitoringInstanceRecord {
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
    last_heartbeat_at: '2026-04-24T09:00:00Z',
    last_sync_at: '2026-04-24T09:05:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

function sample(overrides: Partial<HostSample> = {}): HostSample {
  return {
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-04-24T09:00:00Z',
    received_at: '2026-04-24T09:00:01Z',
    agent_version: 'dev',
    fingerprint: 'fp',
    cpu_usage_pct: 10,
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
    cpu_iowait_pct: 1,
    cpu_steal_pct: 0,
    disk_read_bytes_per_sec: 0,
    disk_write_bytes_per_sec: 0,
    disk_busy_pct: 0,
    uptime_seconds: 3600,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 's',
    ...overrides,
  }
}

function renderNotices(
  overrides: Partial<Parameters<typeof MonitoringDetailNotices>[0]> = {},
) {
  return render(
    <MonitoringDetailNotices
      monitoringInstance={instance()}
      incidents={[]}
      incidentsError={null}
      incidentsLoaded
      incidentsRetrying={false}
      onRetryIncidents={noop}
      runtimeError={null}
      runtimeFactsError={null}
      runtimeFactsLoading={false}
      hasRetainedRuntimeFacts={false}
      onRetryRuntimeFacts={noop}
      bindingConflictLoading={false}
      bindingConflictError={null}
      onRetryBindingConflict={noop}
      onOpenBindingConflict={noop}
      onOpenIncidentHistory={noop}
      snapshotReadAt={new Date('2026-04-24T10:00:00Z')}
      sample={sample()}
      {...overrides}
    />,
  )
}

describe('MonitoringDetailNotices', () => {
  it('keeps the incident action inside the notice well, next to the copy', () => {
    const incidents: ActiveIncidentRecord[] = [{
      incident_id: 'inc_1',
      incident_class: 'resource_threshold',
      object_type: 'monitoring_instance',
      object_id: 'mi_001',
      severity: '严重',
      started_at: '2026-04-24T08:00:00Z',
      last_evaluated_at: '2026-04-24T10:00:00Z',
      source_summary: '磁盘使用率持续超过阈值',
    }]
    const { container } = renderNotices({
      monitoringInstance: instance({
        current_health_status: '严重',
        current_active_incident_count: 2,
        current_primary_issue_summary: '磁盘使用率持续超过阈值',
      }),
      incidents,
    })
    const notice = container.querySelector('.monitoring-detail-notice--critical')
    expect(notice).not.toBeNull()
    expect(notice).toHaveTextContent('严重')
    expect(notice).toHaveTextContent('磁盘使用率持续超过阈值')
    expect(notice?.querySelector('.monitoring-detail-notice__actions')).toContainElement(
      screen.getByRole('button', { name: '查看事件' }),
    )
    expect(container.querySelector('.text-link')).toBeNull()
  })

  it('does not add a second stale strip when the incident is already a heartbeat timeout', () => {
    renderNotices({
      monitoringInstance: instance({
        current_health_status: '告警',
        current_active_incident_count: 1,
        current_primary_issue_summary: '心跳超时',
      }),
      incidents: [{
        incident_id: 'inc_hb',
        incident_class: 'heartbeat_stale',
        object_type: 'monitoring_instance',
        object_id: 'mi_001',
        severity: '告警',
        started_at: '2026-04-24T09:00:00Z',
        last_evaluated_at: '2026-04-24T10:00:00Z',
        source_summary: '心跳超时',
      }],
      heartbeatFreshness: { kind: 'stale', at: '2026-04-24T09:00:00Z' },
    })
    expect(screen.getByText('心跳超时')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看事件' })).toBeInTheDocument()
    expect(screen.queryByText('心跳与采样已落后')).not.toBeInTheDocument()
  })

  it('raises stale heartbeat into its own notice when no incident exists', () => {
    const { container } = renderNotices({
      heartbeatFreshness: { kind: 'stale', at: '2026-04-24T09:30:00Z' },
      sample: sample({ observed_at: '2026-04-24T04:00:00Z' }),
    })
    const notice = container.querySelector('.monitoring-detail-notice--notice')
    expect(notice).toHaveTextContent('数据陈旧')
    expect(notice).toHaveTextContent('心跳与采样已落后')
    expect(screen.queryByRole('button', { name: '查看事件' })).not.toBeInTheDocument()
  })

  it('does not invent a missing-heartbeat notice for a never-bound instance', () => {
    const { container } = renderNotices({
      monitoringInstance: instance({
        binding_status: '未绑定',
        lifecycle_status: '待接入',
      }),
      heartbeatFreshness: { kind: 'missing' },
      sample: null,
    })
    expect(container.querySelector('.monitoring-detail-notice')).toBeNull()
  })

  it('surfaces maintenance and pause as their own wells', () => {
    const maintenance = renderNotices({
      monitoringInstance: instance({ monitoring_status: '维护中' }),
    })
    expect(maintenance.container.querySelector('.monitoring-detail-notice--maintenance')).toHaveTextContent('观测继续，异常通知已抑制')
    maintenance.unmount()

    const paused = renderNotices({
      monitoringInstance: instance({ monitoring_status: '暂停' }),
    })
    expect(paused.container.querySelector('.monitoring-detail-notice--offline')).toHaveTextContent('监控已暂停')
  })

  it('keeps the binding-conflict action inside the notice well', () => {
    const { container } = renderNotices({
      monitoringInstance: instance({ binding_status: '指纹变更待确认' }),
    })
    const notice = container.querySelector('.monitoring-detail-notice--alert')
    expect(notice).toHaveTextContent('绑定冲突待确认')
    expect(notice?.querySelector('.monitoring-detail-notice__actions')).toContainElement(
      screen.getByRole('button', { name: '处置绑定冲突' }),
    )
  })
})
