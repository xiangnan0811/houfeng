import { describe, expect, it } from 'vitest'

import type { IncidentDefaults } from '../../lib/types'
import {
  classifyHeartbeatFreshness,
  heartbeatIsStale,
  heartbeatMissedIntervals,
  readHeartbeatFreshnessPolicy,
} from './heartbeatFreshness'

const policy = { heartbeatIntervalMs: 5000, missingThreshold: 12 }

function defaults(overrides: Partial<IncidentDefaults> = {}): IncidentDefaults {
  return {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 12,
    sweep_interval_seconds: 5,
    notify_on_started: true,
    notify_on_escalated: true,
    notify_on_recovered: true,
    cpu_warning_pct: 80,
    cpu_alert_pct: 90,
    cpu_critical_pct: 95,
    mem_warning_pct: 85,
    mem_alert_pct: 92,
    mem_critical_pct: 95,
    disk_warning_pct: 85,
    disk_alert_pct: 92,
    disk_critical_pct: 97,
    inode_warning_pct: 80,
    inode_alert_pct: 90,
    inode_critical_pct: 95,
    iowait_warning_pct: 20,
    iowait_critical_pct: 50,
    load5_warning: 4,
    load5_critical: 8,
    ...overrides,
  }
}

describe('heartbeat freshness policy', () => {
  it('accepts only positive integer global interval and threshold', () => {
    expect(readHeartbeatFreshnessPolicy(defaults())).toEqual(policy)
    expect(readHeartbeatFreshnessPolicy(defaults({ heartbeat_interval_seconds: 0 }))).toBeNull()
    expect(readHeartbeatFreshnessPolicy(defaults({ stale_threshold_intervals: -1 }))).toBeNull()
    expect(readHeartbeatFreshnessPolicy(null)).toBeNull()
  })

  it('treats missed intervals equal to the threshold as stale', () => {
    const now = new Date('2026-04-26T09:01:00Z')
    const last = new Date('2026-04-26T09:00:00Z')
    expect(heartbeatMissedIntervals(now, last, 5000)).toBe(12)
    expect(heartbeatIsStale(now, last, policy)).toBe(true)
  })

  it('does not treat one interval below the threshold as stale', () => {
    const now = new Date('2026-04-26T09:00:55Z')
    const last = new Date('2026-04-26T09:00:00Z')
    expect(heartbeatMissedIntervals(now, last, 5000)).toBe(11)
    expect(heartbeatIsStale(now, last, policy)).toBe(false)
  })

  it('does not mark future timestamps stale', () => {
    const now = new Date('2026-04-26T09:00:00Z')
    const last = new Date('2026-04-26T09:00:10Z')
    expect(heartbeatIsStale(now, last, policy)).toBe(false)
    expect(classifyHeartbeatFreshness(last.toISOString(), policy, now).kind).toBe('fresh')
  })

  it('keeps timestamps and reports policy unavailable without inventing thresholds', () => {
    const at = '2026-04-26T09:00:00Z'
    expect(classifyHeartbeatFreshness(at, null, new Date('2026-04-26T10:00:00Z'))).toEqual({
      kind: 'policy-unavailable',
      at,
    })
  })

  it('does not classify while settings are unresolved', () => {
    const at = '2026-04-26T09:00:00Z'
    expect(classifyHeartbeatFreshness(at, null, new Date(), false).kind).toBe('pending')
  })

  it('surfaces invalid timestamps instead of calling them stale', () => {
    expect(classifyHeartbeatFreshness('not-a-time', policy, new Date())).toEqual({
      kind: 'invalid',
      raw: 'not-a-time',
    })
  })
})
