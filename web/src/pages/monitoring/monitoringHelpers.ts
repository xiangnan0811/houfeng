import type { BadgeTone, HealthState } from '../../components/atoms'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { formatBytesPerSecond, formatUptime } from '../../lib/format'
import { parseHeartbeatTimestamp } from './heartbeatFreshness'
import type { HeartbeatFreshness, MonitoringInstanceFilterState, MonitoringInstanceQuickView } from './types'

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

const KNOWN_HEALTH: Record<string, HealthState> = {
  正常: 'normal',
  关注: 'notice',
  告警: 'alert',
  严重: 'critical',
}

const HEALTH_TONE: Record<string, BadgeTone> = {
  正常: 'normal',
  关注: 'notice',
  告警: 'alert',
  严重: 'critical',
}

const ABNORMAL_HEALTH: Record<string, true> = {
  关注: true,
  告警: true,
  严重: true,
}

const HEALTH_RANK: Record<string, number> = {
  严重: 4,
  告警: 3,
  关注: 2,
  正常: 1,
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

export function isKnownAbnormalHealth(status: string): boolean {
  return ABNORMAL_HEALTH[status] === true
}

export function isKnownHealthStatus(status: string): boolean {
  return KNOWN_HEALTH[status] !== undefined
}

/** Health glyph is independent of pause/maintenance. No heartbeat is unknown, not historical 正常. */
export function monitoringInstanceHasHeartbeatEvidence(monitoringInstance: MonitoringInstanceRecord): boolean {
  return parseHeartbeatTimestamp(monitoringInstance.last_heartbeat_at ?? undefined) != null
}

export function monitoringInstanceEffectiveHealth(
  monitoringInstance: MonitoringInstanceRecord,
  hasHeartbeat = monitoringInstanceHasHeartbeatEvidence(monitoringInstance),
): string {
  if (!hasHeartbeat) return '未知'
  if (isKnownHealthStatus(monitoringInstance.current_health_status)) {
    return monitoringInstance.current_health_status
  }
  return '未知'
}

export function monitoringInstanceGlyphState(
  monitoringInstance: MonitoringInstanceRecord,
  hasHeartbeat = monitoringInstanceHasHeartbeatEvidence(monitoringInstance),
): HealthState {
  return KNOWN_HEALTH[monitoringInstanceEffectiveHealth(monitoringInstance, hasHeartbeat)] ?? 'offline'
}

export function monitoringInstanceHealthLabel(
  monitoringInstance: MonitoringInstanceRecord,
  hasHeartbeat = monitoringInstanceHasHeartbeatEvidence(monitoringInstance),
): string {
  return monitoringInstanceEffectiveHealth(monitoringInstance, hasHeartbeat)
}

export function monitoringInstanceHealthTone(
  monitoringInstance: MonitoringInstanceRecord,
  hasHeartbeat = monitoringInstanceHasHeartbeatEvidence(monitoringInstance),
): BadgeTone {
  return HEALTH_TONE[monitoringInstanceEffectiveHealth(monitoringInstance, hasHeartbeat)] ?? 'offline'
}

export type MonitoringAttentionBadge = { label: string; tone: BadgeTone }

/** List attention cell: never 正常, never 未知 stacked on 未绑定. */
export function monitoringInstanceAttentionBadges(
  monitoringInstance: MonitoringInstanceRecord,
  hasHeartbeat = monitoringInstanceHasHeartbeatEvidence(monitoringInstance),
): MonitoringAttentionBadge[] {
  const badges: MonitoringAttentionBadge[] = []
  if (monitoringInstance.monitoring_status === '维护中') {
    badges.push({ label: '维护中', tone: 'maintenance' })
  } else if (monitoringInstance.monitoring_status === '暂停') {
    badges.push({ label: '暂停', tone: 'offline' })
  }
  if (monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_UNBOUND_STATUS) {
    badges.push({ label: '未绑定', tone: 'offline' })
  } else if (isBindingConflictMonitoringInstance(monitoringInstance)) {
    badges.push({ label: MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY, tone: 'notice' })
  }
  const health = monitoringInstanceEffectiveHealth(monitoringInstance, hasHeartbeat)
  if (isKnownAbnormalHealth(health)) {
    badges.push({ label: health, tone: HEALTH_TONE[health] ?? 'offline' })
  } else if (health === '未知' && monitoringInstance.binding_status !== MONITORING_INSTANCE_BINDING_UNBOUND_STATUS) {
    badges.push({ label: '未知', tone: 'offline' })
  }
  return badges
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

export function isRuntimeAttentionMonitoringInstance(monitoringInstance: MonitoringInstanceRecord) {
  return monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停'
}

export function countAbnormalMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter((monitoringInstance) => isKnownAbnormalHealth(monitoringInstanceEffectiveHealth(monitoringInstance))).length
}

export function countPendingOnboardingMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter(isPendingOnboardingMonitoringInstance).length
}

export function countMaintenanceOrPausedMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter(isRuntimeAttentionMonitoringInstance).length
}

export function countBindingConflictMonitoringInstances(monitoring: MonitoringInstanceRecord[]) {
  return monitoring.filter(isBindingConflictMonitoringInstance).length
}

export function matchesMonitoringQuickView(
  monitoringInstance: MonitoringInstanceRecord,
  view: MonitoringInstanceQuickView,
): boolean {
  if (view === 'all') return true
  if (view === 'abnormal') return isKnownAbnormalHealth(monitoringInstanceEffectiveHealth(monitoringInstance))
  if (view === 'onboarding') return isPendingOnboardingMonitoringInstance(monitoringInstance)
  if (view === 'runtime-attention') return isRuntimeAttentionMonitoringInstance(monitoringInstance)
  return isBindingConflictMonitoringInstance(monitoringInstance)
}

export function matchesMonitoringFilters(
  monitoringInstance: MonitoringInstanceRecord,
  filters: MonitoringInstanceFilterState,
): boolean {
  if (filters.group && monitoringInstance.group !== filters.group) return false
  if (filters.region && monitoringInstance.region !== filters.region) return false
  if (filters.city && monitoringInstance.city !== filters.city) return false
  if (filters.provider && monitoringInstance.provider !== filters.provider) return false
  if (filters.lifecycle && monitoringInstance.lifecycle_status !== filters.lifecycle) return false
  if (filters.runStatus && monitoringInstance.monitoring_status !== filters.runStatus) return false
  if (filters.health && monitoringInstanceEffectiveHealth(monitoringInstance) !== filters.health) return false
  if (filters.labels.length > 0) {
    const hasAll = filters.labels.every((label) => monitoringInstance.labels.includes(label))
    if (!hasAll) return false
  }
  return true
}

export function matchesMonitoringSearch(monitoringInstance: MonitoringInstanceRecord, query: string): boolean {
  const needle = query.trim().toLocaleLowerCase('zh-Hans-CN')
  if (!needle) return true
  const haystack = [
    monitoringInstance.display_name,
    monitoringInstance.monitoring_instance_id,
    monitoringInstance.group,
    monitoringInstance.region,
    monitoringInstance.city,
    monitoringInstance.provider,
    monitoringInstance.note,
    ...monitoringInstance.labels,
  ]
    .join('\n')
    .toLocaleLowerCase('zh-Hans-CN')
  return haystack.includes(needle)
}

export function monitoringIssueSummary(monitoringInstance: MonitoringInstanceRecord): string {
  if (isBindingConflictMonitoringInstance(monitoringInstance)) return MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY
  return monitoringInstance.current_primary_issue_summary.trim()
}

export function monitoringLocationLine(monitoringInstance: MonitoringInstanceRecord): string {
  return [monitoringInstance.group, monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider]
    .filter(Boolean)
    .join(' · ')
}

export function compareMonitoringHealth(
  left: MonitoringInstanceRecord,
  right: MonitoringInstanceRecord,
): number {
  const leftRank = HEALTH_RANK[monitoringInstanceEffectiveHealth(left)] ?? 0
  const rightRank = HEALTH_RANK[monitoringInstanceEffectiveHealth(right)] ?? 0
  return leftRank - rightRank
}

export function compareMonitoringHeartbeat(
  left: HeartbeatFreshness,
  right: HeartbeatFreshness,
): number {
  const leftTime = heartbeatSortTime(left)
  const rightTime = heartbeatSortTime(right)
  if (leftTime === rightTime) return 0
  if (leftTime === 0) return 1
  if (rightTime === 0) return -1
  return leftTime - rightTime
}

function heartbeatSortTime(freshness: HeartbeatFreshness): number {
  if (freshness.kind === 'missing' || freshness.kind === 'invalid') return 0
  const parsed = Date.parse(freshness.at)
  return Number.isNaN(parsed) ? 0 : parsed
}

export function formatNetworkRate(rate?: number | null): string {
  if (rate == null || Number.isNaN(rate) || rate < 0) return '—'
  return formatBytesPerSecond(rate)
}

export function formatSampledUptime(seconds?: number | null): string {
  if (seconds == null || Number.isNaN(seconds) || seconds < 0) return '—'
  return formatUptime(seconds)
}
