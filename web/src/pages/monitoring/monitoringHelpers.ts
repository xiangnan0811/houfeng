import type { HealthState } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'
import type { CreateMonitoringInstanceInput, MonitoringInstanceRecord } from '../../lib/types'
import type { MonitoringInstanceEvidenceItem, MonitoringInstanceEvidenceLead, MonitoringInstanceFilterState, MonitoringInstanceRuntimeAction } from './types'

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

export const initialCreateForm: CreateMonitoringInstanceInput = {
  display_name: '',
  group: '',
  region: '',
  city: '',
  provider: '',
  labels: [],
  note: '',
}

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

export function monitoringInstanceEvidenceGlyphState(monitoringInstance: MonitoringInstanceRecord): HealthState {
  if (monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停') {
    return monitoringInstanceGlyphState(monitoringInstance)
  }
  if (isBindingConflictMonitoringInstance(monitoringInstance) || isPendingOnboardingMonitoringInstance(monitoringInstance)) return 'notice'
  return monitoringInstanceGlyphState(monitoringInstance)
}

export function parseLabels(value: string) {
  const result: string[] = []
  const seen = new Set<string>()

  for (const label of value.split(/[,，]/).map((item) => item.trim()).filter(Boolean)) {
    if (seen.has(label)) continue
    seen.add(label)
    result.push(label)
  }

  return result
}

export function monitoringInstanceRuntimeActions(
  monitoringInstance: MonitoringInstanceRecord,
): Array<{ action: MonitoringInstanceRuntimeAction; label: string; variant?: 'danger' }> {
  const actions: Array<{ action: MonitoringInstanceRuntimeAction; label: string; variant?: 'danger' }> = []

  if (monitoringInstance.monitoring_status === '启用') {
    actions.push(
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停监控' },
    )
  } else if (monitoringInstance.monitoring_status === '维护中') {
    actions.push(
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停监控' },
    )
  } else if (monitoringInstance.monitoring_status === '暂停') {
    actions.push({ action: 'resume', label: '恢复监控' })
  }

  if (monitoringInstance.lifecycle_status === '在用' || monitoringInstance.lifecycle_status === '观察中' || monitoringInstance.lifecycle_status === '不续费') {
    actions.push({ action: 'retire', label: '退役', variant: 'danger' })
  } else if (monitoringInstance.lifecycle_status === '已退役') {
    actions.push({ action: 'restore-to-observing', label: '恢复观察' })
  }

  return actions
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

function monitoringInstanceAttentionRank(monitoringInstance: MonitoringInstanceRecord): number {
  if (monitoringInstance.current_health_status === '严重') return 0
  if (monitoringInstance.current_health_status === '告警') return 1
  if (isBindingConflictMonitoringInstance(monitoringInstance)) return 2
  if (monitoringInstance.current_health_status === '关注') return 3
  if (isPendingOnboardingMonitoringInstance(monitoringInstance)) return 4
  if (monitoringInstance.monitoring_status === '维护中') return 5
  if (monitoringInstance.monitoring_status === '暂停') return 6
  return 9
}

function monitoringInstanceEvidenceReason(monitoringInstance: MonitoringInstanceRecord): string {
  if (isBindingConflictMonitoringInstance(monitoringInstance)) return MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY
  if (monitoringInstance.current_primary_issue_summary) return monitoringInstance.current_primary_issue_summary
  if (monitoringInstance.current_health_status !== '正常') return `健康状态：${monitoringInstance.current_health_status}`
  if (isPendingOnboardingMonitoringInstance(monitoringInstance)) return '接入 / 绑定未完成'
  if (monitoringInstance.monitoring_status === '维护中') return '维护窗口'
  if (monitoringInstance.monitoring_status === '暂停') return '监控暂停'
  return '当前没有明显异常'
}

function monitoringInstanceEvidenceMeta(monitoringInstance: MonitoringInstanceRecord): string {
  const location = [monitoringInstance.group, monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider].filter(Boolean).join(' · ')
  const freshness = monitoringInstance.last_heartbeat_at
    ? `心跳 ${formatDateTime(monitoringInstance.last_heartbeat_at)}`
    : '尚无心跳'
  const incident = `活跃异常 ${monitoringInstance.current_active_incident_count}`
  return [location || '未标记位置', freshness, incident].join(' · ')
}

export function pickTopMonitoringInstanceEvidence(monitoring: MonitoringInstanceRecord[]): MonitoringInstanceEvidenceItem | null {
  if (monitoring.length === 0) return null
  const candidates = [...monitoring]
    .filter(
      (monitoringInstance) =>
        monitoringInstance.current_health_status !== '正常' ||
        isPendingOnboardingMonitoringInstance(monitoringInstance) ||
        monitoringInstance.monitoring_status === '维护中' ||
        monitoringInstance.monitoring_status === '暂停',
    )
    .sort((a, b) => {
      const rankDiff = monitoringInstanceAttentionRank(a) - monitoringInstanceAttentionRank(b)
      if (rankDiff !== 0) return rankDiff
      const incidentDiff = b.current_active_incident_count - a.current_active_incident_count
      if (incidentDiff !== 0) return incidentDiff
      return a.display_name.localeCompare(b.display_name, 'zh-Hans-CN')
    })

  const monitoringInstance = candidates[0]
  if (!monitoringInstance) return null

  return {
    monitoringInstance,
    title: monitoringInstance.display_name || monitoringInstance.monitoring_instance_id,
    reason: monitoringInstanceEvidenceReason(monitoringInstance),
    meta: monitoringInstanceEvidenceMeta(monitoringInstance),
    route: isPendingOnboardingMonitoringInstance(monitoringInstance) ? `/monitoring/${monitoringInstance.monitoring_instance_id}?onboarding=1` : `/monitoring/${monitoringInstance.monitoring_instance_id}`,
    actionLabel: isPendingOnboardingMonitoringInstance(monitoringInstance) ? '处理接入' : '查看证据',
  }
}

export function describeMonitoringInstanceFilterContext(filterState: MonitoringInstanceFilterState): string[] {
  const items: string[] = []
  if (filterState.group) items.push(`Group ${filterState.group}`)
  if (filterState.region) items.push(`地区 ${filterState.region}`)
  if (filterState.city) items.push(`城市 ${filterState.city}`)
  if (filterState.provider) items.push(`供应商 ${filterState.provider}`)
  if (filterState.lifecycle) items.push(`生命周期 ${filterState.lifecycle}`)
  if (filterState.runStatus) items.push(`运行 ${filterState.runStatus}`)
  if (filterState.health) items.push(`健康 ${filterState.health}`)
  for (const label of filterState.labels) items.push(`标签 ${label}`)
  if (filterState.abnormal) items.push('仅看异常')
  if (filterState.onboardingPending) items.push('待接入/绑定')
  return items
}

export function buildMonitoringInstanceEvidenceLead(args: {
  totalMonitoringInstanceCount: number
  displayedMonitoringInstanceCount: number
  abnormalMonitoringInstanceCount: number
  pendingOnboardingMonitoringInstanceCount: number
  maintenanceOrPausedMonitoringInstanceCount: number
  hasActiveFilters: boolean
}): MonitoringInstanceEvidenceLead {
  const {
    totalMonitoringInstanceCount,
    displayedMonitoringInstanceCount,
    abnormalMonitoringInstanceCount,
    pendingOnboardingMonitoringInstanceCount,
    maintenanceOrPausedMonitoringInstanceCount,
    hasActiveFilters,
  } = args

  if (displayedMonitoringInstanceCount === 0 && hasActiveFilters) {
    return {
      eyebrow: '当前筛选',
      title: '没有匹配当前证据条件',
      description: '调整筛选。',
      actionKind: 'clear',
      actionLabel: '清空证据筛选',
      tone: 'offline',
    }
  }

  if (totalMonitoringInstanceCount === 0) {
    return {
      eyebrow: '首次接入',
      title: '先接入第一条监控实例证据',
      description: '创建并接入。',
      actionKind: 'create',
      actionLabel: '接入监控实例',
      tone: 'notice',
    }
  }

  if (abnormalMonitoringInstanceCount > 0) {
    return {
      eyebrow: '优先证据',
      title: `先处理 ${abnormalMonitoringInstanceCount} 个异常监控实例`,
      description: '进详情处理。',
      actionKind: 'abnormal',
      actionLabel: '聚焦异常证据',
      tone: 'alert',
    }
  }

  if (pendingOnboardingMonitoringInstanceCount > 0) {
    return {
      eyebrow: '接入证据',
      title: `补齐 ${pendingOnboardingMonitoringInstanceCount} 个接入 / 绑定状态`,
      description: '未完成。',
      actionKind: 'onboarding',
      actionLabel: '聚焦接入证据',
      tone: 'notice',
    }
  }

  if (maintenanceOrPausedMonitoringInstanceCount > 0) {
    return {
      eyebrow: '运行上下文',
      title: `核对 ${maintenanceOrPausedMonitoringInstanceCount} 个维护 / 暂停监控实例`,
      description: '维护 / 暂停空窗。',
      actionKind: 'runtime',
      actionLabel: '聚焦运行证据',
      tone: 'maintenance',
    }
  }

  return {
    eyebrow: '证据稳定',
    title: '监控实例运行证据当前稳定',
    description: '无异常 / 接入缺口。',
    actionKind: 'asset',
    actionLabel: '查看 VPS 库存',
    tone: 'normal',
  }
}

export function isRuntimeAttentionMonitoringInstance(monitoringInstance: MonitoringInstanceRecord) {
  return monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停'
}

export function pauseConfirmationCurrent(monitoringInstance: MonitoringInstanceRecord) {
  return monitoringInstance.monitoring_status === '维护中'
    ? '当前：监控运行状态为维护中。'
    : '当前：监控运行状态为启用。'
}

export function mergeNonMetadataMonitoringInstanceRecord(current: MonitoringInstanceRecord, updated: MonitoringInstanceRecord): MonitoringInstanceRecord {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

export function actionButtonKey(monitoringInstanceId: string, action: MonitoringInstanceRuntimeAction) {
  return `${monitoringInstanceId}:${action}`
}
