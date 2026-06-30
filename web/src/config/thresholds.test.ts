import { describe, expect, it } from 'vitest'

import { DEFAULT_THRESHOLDS, resolveThresholds } from './thresholds'

describe('thresholds', () => {
  it('matches backend incident defaults with warning alert and critical tiers', () => {
    expect(DEFAULT_THRESHOLDS.disk).toEqual({ warning: 85, alert: 92, critical: 97 })
    expect(DEFAULT_THRESHOLDS.cpu).toEqual({ warning: 80, alert: 90, critical: 95 })
    expect(DEFAULT_THRESHOLDS.mem).toEqual({ warning: 85, alert: 92, critical: 95 })
    expect(DEFAULT_THRESHOLDS.inode).toEqual({ warning: 80, alert: 90, critical: 95 })
  })

  it('resolves runtime settings and derives alert for two-level iowait and load thresholds', () => {
    expect(resolveThresholds({
      cpu_warning_pct: 70,
      cpu_alert_pct: 82,
      cpu_critical_pct: 93,
      disk_warning_pct: 81,
      disk_alert_pct: 88,
      disk_critical_pct: 96,
      iowait_warning_pct: 10,
      iowait_critical_pct: 40,
      load5_warning: 2,
      load5_critical: 10,
    })).toMatchObject({
      cpu: { warning: 70, alert: 82, critical: 93 },
      disk: { warning: 81, alert: 88, critical: 96 },
      iowait: { warning: 10, alert: 25, critical: 40 },
      load5: { warning: 2, alert: 6, critical: 10 },
    })
  })

  it('falls back to defaults when runtime thresholds are misordered', () => {
    expect(resolveThresholds({
      cpu_warning_pct: 95,
      cpu_alert_pct: 80,
      cpu_critical_pct: 90,
      iowait_warning_pct: 50,
      iowait_critical_pct: 20,
      load5_warning: 9,
      load5_critical: 4,
    })).toMatchObject({
      cpu: DEFAULT_THRESHOLDS.cpu,
      iowait: DEFAULT_THRESHOLDS.iowait,
      load5: DEFAULT_THRESHOLDS.load5,
    })
  })
})
