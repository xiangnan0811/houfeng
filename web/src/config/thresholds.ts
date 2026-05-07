/**
 * Default metric thresholds — match backend IncidentDefaults in
 * internal/center/settings/types.go. Edit this file to change defaults;
 * runtime overrides from /api/settings take precedence when loaded.
 */
export const DEFAULT_THRESHOLDS = {
  cpu:  { notice: 80, critical: 95 },
  mem:  { notice: 85, critical: 95 },
  disk: { notice: 80, critical: 95 },
  inode: { notice: 80, critical: 95 },
  iowait: { notice: 20, critical: 50 },
  load5: { notice: 4.0, critical: 8.0 },
} as const

export type MetricThresholdKey = keyof typeof DEFAULT_THRESHOLDS

export interface MetricThresholds {
  cpu:  { notice: number; critical: number }
  mem:  { notice: number; critical: number }
  disk: { notice: number; critical: number }
  inode:{ notice: number; critical: number }
  iowait:{ notice: number; critical: number }
  load5:{ notice: number; critical: number }
}

/**
 * Resolve thresholds from the API settings response.
 * Falls back to DEFAULT_THRESHOLDS for any missing or zero values.
 */
export function resolveThresholds(incidentDefaults?: {
  cpu_warning_pct?: number
  cpu_critical_pct?: number
  mem_warning_pct?: number
  mem_critical_pct?: number
  disk_warning_pct?: number
  disk_critical_pct?: number
  inode_warning_pct?: number
  inode_critical_pct?: number
  iowait_warning_pct?: number
  iowait_critical_pct?: number
  load5_warning?: number
  load5_critical?: number
} | null): MetricThresholds {
  const d = DEFAULT_THRESHOLDS
  if (!incidentDefaults) return { ...d }

  return {
    cpu:  { notice: incidentDefaults.cpu_warning_pct  || d.cpu.notice,  critical: incidentDefaults.cpu_critical_pct  || d.cpu.critical },
    mem:  { notice: incidentDefaults.mem_warning_pct  || d.mem.notice,  critical: incidentDefaults.mem_critical_pct  || d.mem.critical },
    disk: { notice: incidentDefaults.disk_warning_pct || d.disk.notice, critical: incidentDefaults.disk_critical_pct || d.disk.critical },
    inode:{ notice: incidentDefaults.inode_warning_pct|| d.inode.notice,critical: incidentDefaults.inode_critical_pct|| d.inode.critical },
    iowait:{ notice: incidentDefaults.iowait_warning_pct|| d.iowait.notice,critical: incidentDefaults.iowait_critical_pct|| d.iowait.critical },
    load5:{ notice: incidentDefaults.load5_warning   || d.load5.notice, critical: incidentDefaults.load5_critical   || d.load5.critical },
  }
}
