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

function orderedThreeLevelThreshold(
  warning: number | undefined,
  alert: number | undefined,
  critical: number | undefined,
  defaults: MetricThreshold,
): MetricThreshold {
  const resolved = {
    warning: warning || defaults.warning,
    alert: alert || defaults.alert,
    critical: critical || defaults.critical,
  }
  if (!(resolved.warning < resolved.alert && resolved.alert < resolved.critical)) {
    return defaults
  }
  return resolved
}

function orderedTwoLevelThreshold(
  warning: number | undefined,
  critical: number | undefined,
  defaults: MetricThreshold,
): MetricThreshold {
  const resolvedWarning = warning || defaults.warning
  const resolvedCritical = critical || defaults.critical
  if (!(resolvedWarning < resolvedCritical)) {
    return defaults
  }
  return {
    warning: resolvedWarning,
    alert: midpoint(resolvedWarning, resolvedCritical),
    critical: resolvedCritical,
  }
}

function midpoint(warning: number, critical: number) {
  return warning + (critical - warning) / 2
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

  return {
    cpu: orderedThreeLevelThreshold(incidentDefaults.cpu_warning_pct, incidentDefaults.cpu_alert_pct, incidentDefaults.cpu_critical_pct, d.cpu),
    mem: orderedThreeLevelThreshold(incidentDefaults.mem_warning_pct, incidentDefaults.mem_alert_pct, incidentDefaults.mem_critical_pct, d.mem),
    disk: orderedThreeLevelThreshold(incidentDefaults.disk_warning_pct, incidentDefaults.disk_alert_pct, incidentDefaults.disk_critical_pct, d.disk),
    inode: orderedThreeLevelThreshold(incidentDefaults.inode_warning_pct, incidentDefaults.inode_alert_pct, incidentDefaults.inode_critical_pct, d.inode),
    iowait: orderedTwoLevelThreshold(incidentDefaults.iowait_warning_pct, incidentDefaults.iowait_critical_pct, d.iowait),
    load5: orderedTwoLevelThreshold(incidentDefaults.load5_warning, incidentDefaults.load5_critical, d.load5),
  }
}
