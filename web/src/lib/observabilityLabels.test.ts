import { describe, expect, it } from 'vitest'

import { incidentClassLabel, severityTone } from './observabilityLabels'

describe('observabilityLabels', () => {
  it('maps known classes to Chinese and never returns snake_case', () => {
    expect(incidentClassLabel('resource_threshold')).toBe('资源阈值')
    expect(incidentClassLabel('heartbeat_stale')).toBe('心跳超时')
    expect(incidentClassLabel('monitoring_instance_disk_pressure')).toBe('监控实例磁盘压力')
    expect(incidentClassLabel('connectivity')).toBe('连通性')
    expect(incidentClassLabel('certificate')).toBe('证书')
    expect(incidentClassLabel('unknown_class')).toBe('异常')
    expect(incidentClassLabel('')).toBe('')
  })

  it('maps severity words onto notice tones', () => {
    expect(severityTone('严重')).toBe('critical')
    expect(severityTone('告警')).toBe('alert')
    expect(severityTone('关注')).toBe('notice')
    expect(severityTone('')).toBe('offline')
  })
})
