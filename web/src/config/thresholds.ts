/**
 * Default metric thresholds — match backend IncidentDefaults in
 * internal/center/settings/types.go. Edit this file to change defaults;
 * runtime overrides from /api/settings take precedence when loaded.
 */
export const DEFAULT_THRESHOLDS = {
  cpu: { warning: 80, alert: 90, critical: 95 },
  mem: { warning: 85, alert: 92, critical: 95 },
  disk: { warning: 85, alert: 92, critical: 97 },
  inode: { warning: 80, alert: 90, critical: 95 },
  iowait: { warning: 20, alert: 35, critical: 50 },
  load5: { warning: 4.0, alert: 6.0, critical: 8.0 },
} as const

export type MetricThresholdKey = keyof typeof DEFAULT_THRESHOLDS

export type MetricThreshold = { warning: number; alert: number; critical: number }

export interface MetricThresholds {
  cpu: MetricThreshold
  mem: MetricThreshold
  disk: MetricThreshold
  inode: MetricThreshold
  iowait: MetricThreshold
  load5: MetricThreshold
}

/**
 * Resolve thresholds from the API settings response.
 * Falls back to DEFAULT_THRESHOLDS for any missing or zero values.
 */
export function resolveThresholds(incidentDefaults?: {
  cpu_warning_pct?: number
  cpu_alert_pct?: number
  cpu_critical_pct?: number
  mem_warning_pct?: number
  mem_alert_pct?: number
  mem_critical_pct?: number
  disk_warning_pct?: number
  disk_alert_pct?: number
  disk_critical_pct?: number
  inode_warning_pct?: number
  inode_alert_pct?: number
  inode_critical_pct?: number
  iowait_warning_pct?: number
  iowait_critical_pct?: number
  load5_warning?: number
  load5_critical?: number
} | null): MetricThresholds {
  const d = DEFAULT_THRESHOLDS
  if (!incidentDefaults) return { ...d }

  const midpoint = (warning: number, critical: number) => warning + (critical - warning) / 2
  const iowaitWarning = incidentDefaults.iowait_warning_pct || d.iowait.warning
  const iowaitCritical = incidentDefaults.iowait_critical_pct || d.iowait.critical
  const load5Warning = incidentDefaults.load5_warning || d.load5.warning
  const load5Critical = incidentDefaults.load5_critical || d.load5.critical

  return {
    cpu: {
      warning: incidentDefaults.cpu_warning_pct || d.cpu.warning,
      alert: incidentDefaults.cpu_alert_pct || d.cpu.alert,
      critical: incidentDefaults.cpu_critical_pct || d.cpu.critical,
    },
    mem: {
      warning: incidentDefaults.mem_warning_pct || d.mem.warning,
      alert: incidentDefaults.mem_alert_pct || d.mem.alert,
      critical: incidentDefaults.mem_critical_pct || d.mem.critical,
    },
    disk: {
      warning: incidentDefaults.disk_warning_pct || d.disk.warning,
      alert: incidentDefaults.disk_alert_pct || d.disk.alert,
      critical: incidentDefaults.disk_critical_pct || d.disk.critical,
    },
    inode: {
      warning: incidentDefaults.inode_warning_pct || d.inode.warning,
      alert: incidentDefaults.inode_alert_pct || d.inode.alert,
      critical: incidentDefaults.inode_critical_pct || d.inode.critical,
    },
    iowait: {
      warning: iowaitWarning,
      alert: midpoint(iowaitWarning, iowaitCritical),
      critical: iowaitCritical,
    },
    load5: {
      warning: load5Warning,
      alert: midpoint(load5Warning, load5Critical),
      critical: load5Critical,
    },
  }
}
