import { describe, expect, it } from 'vitest'

import type { MonitoringInstanceRecord } from '../../lib/types'
import {
  compareMonitoringHealth,
  countAbnormalMonitoringInstances,
  formatNetworkRate,
  formatSampledUptime,
  matchesMonitoringFilters,
  matchesMonitoringQuickView,
  monitoringInstanceAttentionBadges,
  monitoringInstanceEffectiveHealth,
  monitoringInstanceGlyphState,
  monitoringInstanceHealthLabel,
  monitoringIssueSummary,
} from './monitoringHelpers'

function record(overrides: Partial<MonitoringInstanceRecord> = {}): MonitoringInstanceRecord {
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
    ...overrides,
  }
}

describe('monitoring list health helpers', () => {
  it('counts only known abnormal health 关注/告警/严重 with heartbeat evidence', () => {
    expect(countAbnormalMonitoringInstances([
      record({ current_health_status: '正常', last_heartbeat_at: '2026-04-26T09:00:00Z' }),
      record({ monitoring_instance_id: 'mi_002', current_health_status: '关注', last_heartbeat_at: '2026-04-26T09:00:00Z' }),
      record({ monitoring_instance_id: 'mi_003', current_health_status: '告警', last_heartbeat_at: '2026-04-26T09:00:00Z' }),
      record({ monitoring_instance_id: 'mi_004', current_health_status: '严重', last_heartbeat_at: '2026-04-26T09:00:00Z' }),
      record({ monitoring_instance_id: 'mi_005', current_health_status: '未知', last_heartbeat_at: '2026-04-26T09:00:00Z' }),
      record({ monitoring_instance_id: 'mi_006', current_health_status: '' }),
    ])).toBe(3)
  })

  it('treats absent or invalid heartbeat as 未知 for count, view, filter and sort', () => {
    const rawAlert = record({ current_health_status: '告警' })
    const liveAlert = record({
      monitoring_instance_id: 'mi_live',
      current_health_status: '告警',
      last_heartbeat_at: '2026-04-26T09:00:00Z',
    })
    const invalid = record({
      monitoring_instance_id: 'mi_bad',
      current_health_status: '严重',
      last_heartbeat_at: 'not-a-time',
    })
    expect(monitoringInstanceEffectiveHealth(rawAlert)).toBe('未知')
    expect(countAbnormalMonitoringInstances([rawAlert, liveAlert, invalid])).toBe(1)
    expect(matchesMonitoringQuickView(rawAlert, 'abnormal')).toBe(false)
    expect(matchesMonitoringQuickView(liveAlert, 'abnormal')).toBe(true)
    expect(matchesMonitoringFilters(rawAlert, {
      group: null, region: null, city: null, provider: null, lifecycle: null, runStatus: null, health: '告警', labels: [],
    })).toBe(false)
    expect(compareMonitoringHealth(rawAlert, liveAlert)).toBeLessThan(0)
  })

  it('does not let pause override health glyph when a heartbeat exists', () => {
    const paused = record({ monitoring_status: '暂停', current_health_status: '告警' })
    expect(monitoringInstanceGlyphState(paused, true)).toBe('alert')
    expect(monitoringInstanceHealthLabel(paused, true)).toBe('告警')
  })

  it('shows unknown rather than historical normal when there is no heartbeat', () => {
    const healthyWithoutHeartbeat = record({ current_health_status: '正常' })
    expect(monitoringInstanceGlyphState(healthyWithoutHeartbeat, false)).toBe('offline')
    expect(monitoringInstanceHealthLabel(healthyWithoutHeartbeat, false)).toBe('未知')
  })

  it('keeps list attention badges silent on 正常 and unstacked on control states', () => {
    expect(monitoringInstanceAttentionBadges(record({
      current_health_status: '正常',
      last_heartbeat_at: '2026-04-26T09:00:00Z',
    }), true)).toEqual([])
    expect(monitoringInstanceAttentionBadges(record({
      current_health_status: '正常',
      monitoring_status: '维护中',
      last_heartbeat_at: '2026-04-26T09:00:00Z',
    }), true)).toEqual([{ label: '维护中', tone: 'maintenance' }])
    expect(monitoringInstanceAttentionBadges(record({
      current_health_status: '告警',
      monitoring_status: '暂停',
      last_heartbeat_at: '2026-04-26T09:00:00Z',
    }), true)).toEqual([
      { label: '暂停', tone: 'offline' },
      { label: '告警', tone: 'alert' },
    ])
    expect(monitoringInstanceAttentionBadges(record({
      current_health_status: '正常',
      binding_status: '未绑定',
    }), false)).toEqual([{ label: '未绑定', tone: 'offline' }])
    expect(monitoringInstanceAttentionBadges(record({
      current_health_status: '正常',
      monitoring_status: '暂停',
    }), false)).toEqual([
      { label: '暂停', tone: 'offline' },
      { label: '未知', tone: 'offline' },
    ])
  })

  it('does not invent empty issue copy for healthy heartbeat rows', () => {
    expect(monitoringIssueSummary(record({ current_health_status: '正常' }))).toBe('')
    expect(monitoringIssueSummary(record({
      last_heartbeat_at: '2026-04-26T09:00:00Z',
    }))).toBe('')
  })
})

describe('monitoring list runtime summary helpers', () => {
  it('formats network rates with true 0 preserved and invalid/missing as em-dash', () => {
    expect(formatNetworkRate(0)).toBe('0 B/s')
    expect(formatNetworkRate(1024)).toBe('1.0 KB/s')
    expect(formatNetworkRate(1048576)).toBe('1.0 MB/s')
    expect(formatNetworkRate(null)).toBe('—')
    expect(formatNetworkRate(undefined)).toBe('—')
    expect(formatNetworkRate(Number.NaN)).toBe('—')
    expect(formatNetworkRate(-1)).toBe('—')
  })

  it('formats sampled uptime without client clock increments and invalid/missing as em-dash', () => {
    expect(formatSampledUptime(0)).toBe('不足 1 分钟')
    expect(formatSampledUptime(3600)).toBe('1小时 0分钟')
    expect(formatSampledUptime(86400 * 3 + 7200)).toBe('3天 2小时')
    expect(formatSampledUptime(null)).toBe('—')
    expect(formatSampledUptime(undefined)).toBe('—')
    expect(formatSampledUptime(Number.NaN)).toBe('—')
    expect(formatSampledUptime(-10)).toBe('—')
  })
})
