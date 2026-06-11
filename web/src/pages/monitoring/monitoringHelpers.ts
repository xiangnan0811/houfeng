import type { HealthState } from '../../components/atoms'
import type { MonitoringInstanceRecord } from '../../lib/types'

export const MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS = [
  { value: '待接入', label: '待接入' },
  { value: '在用', label: '在用' },
  { value: '观察中', label: '观察中' },
  { value: '不续费', label: '不续费' },
  { value: '已退役', label: '已退役' },
] as const

export const MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '暂停', label: '暂停' },
  { value: '维护中', label: '维护中' },
] as const

export const MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS = [
  { value: '正常', label: '正常' },
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
] as const

export const MONITORING_INSTANCE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
export const MONITORING_INSTANCE_BINDING_UNBOUND_STATUS = '未绑定'
export const MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY = '等待绑定确认'

export function parseMultiValue(value: string | null): string[] {
  if (!value) return []
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function distinctSorted(values: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const value of values) {
    if (!value) continue
    if (!seen.has(value)) {
      seen.add(value)
      out.push(value)
    }
  }
  return out.sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

/** Map backend Chinese health-status into the StatusGlyph state vocabulary.
 *  Health is derived (正常/关注/告警/严重) per architecture-data-model §monitoringInstance;
 *  monitoring 维护中/暂停 visually outranks health for at-a-glance scanning. */
export function monitoringInstanceGlyphState(monitoringInstance: MonitoringInstanceRecord): HealthState {
  if (monitoringInstance.monitoring_status === '维护中') return 'maintenance'
  if (monitoringInstance.monitoring_status === '暂停') return 'offline'
  switch (monitoringInstance.current_health_status) {
    case '正常':
      return 'normal'
    case '关注':
      return 'notice'
    case '告警':
      return 'alert'
    case '严重':
      return 'critical'
    default:
      return 'offline'
  }
}

export function isBindingConflictMonitoringInstance(monitoringInstance: MonitoringInstanceRecord) {
  return monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
}

export function isPendingOnboardingMonitoringInstance(monitoringInstance: MonitoringInstanceRecord) {
  return (
    monitoringInstance.lifecycle_status === '待接入' ||
    monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_UNBOUND_STATUS ||
    monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
  )
}

export function countAbnormalMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter((monitoringInstance) => monitoringInstance.current_health_status !== '正常').length
}

export function countPendingOnboardingMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter(isPendingOnboardingMonitoringInstance).length
}

export function countMaintenanceOrPausedMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter(
    (monitoringInstance) => monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停',
  ).length
}

export function isRuntimeAttentionMonitoringInstance(monitoringInstance: MonitoringInstanceRecord) {
  return monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停'
}
