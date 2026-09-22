import type { HealthState } from '../../components/atoms'
import type { MonitoringInstanceRuntimeAction } from '../../components/monitoring-detail'
import { ApiError } from '../../lib/api'
import type {
  MonitoringInstanceOnboardingState,
  MonitoringInstanceRecord,
  PendingBindingMetadata,
  VPSSummary,
} from '../../lib/types'
import { MONITORING_INSTANCE_BINDING_CONFLICT_STATUS } from './monitoringDetailConstants'
import type { MonitoringDetailPageState } from './types'

export const INITIAL_MONITORING_DETAIL_STATE: MonitoringDetailPageState = {
  requestedMonitoringInstanceId: null,
  error: null,
  monitoringInstance: null,
  runtimeFacts: null,
  requestedIncidentsMonitoringInstanceId: null,
  incidents: [],
  incidentsError: null,
  requestedEventsMonitoringInstanceId: null,
  events: [],
  eventsError: null,
}

export function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
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

export function maskFingerprint(value?: string | null) {
  if (!value) return '尚无'
  const normalized = value.trim()
  if (!normalized) return '尚无'
  if (normalized.length <= 14) return normalized
  return `${normalized.slice(0, 8)}…${normalized.slice(-6)}`
}

export function currentFingerprintSummary(onboarding: MonitoringInstanceOnboardingState | null) {
  if (onboarding?.current_binding_fingerprint_summary?.trim()) {
    return onboarding.current_binding_fingerprint_summary.trim()
  }
  return '服务端当前未提供已绑定指纹摘要'
}

export function pendingBindingMetadata(onboarding: MonitoringInstanceOnboardingState | null): PendingBindingMetadata | null {
  return onboarding?.pending_binding ?? null
}

const CLOUD_REGION_CODE_PATTERNS = [/^[a-z]{1,3}-[a-z0-9-]+$/, /^[a-z]{3}\d+$/]

/**
 * Cloud region identifiers (`ap-northeast-1`, `sgp1`, `JP`) are not what an operator
 * reads as a location; the city is. Matching is case-insensitive after trimming.
 */
export function isCloudRegionCode(value: string): boolean {
  const trimmed = value.trim().toLowerCase()
  if (!trimmed) return false
  if (CLOUD_REGION_CODE_PATTERNS.some((pattern) => pattern.test(trimmed))) return true
  return /^[a-z]{1,3}$/.test(trimmed)
}

/** Empty and unconfirmed providers render nothing — never a "Provider 未确认" sentence. */
export function formatMonitoringInstanceProvider(provider: string | null | undefined): string {
  const trimmed = (provider ?? '').trim()
  if (!trimmed || trimmed === '未确认' || trimmed.startsWith('Provider')) return ''
  return trimmed
}

/** Region and city are filtered independently, then joined. Region codes are omitted. */
export function formatMonitoringInstanceLocation(
  region: string | null | undefined,
  city: string | null | undefined,
): string {
  return [region, city]
    .map((part) => (part ?? '').trim())
    .filter((part) => Boolean(part) && part !== '未确认' && !isCloudRegionCode(part))
    .join(' · ')
}

export function monitoringInstanceRuntimeActions(monitoringInstance: MonitoringInstanceRecord): Array<{ action: MonitoringInstanceRuntimeAction; label: string }> {
  if (monitoringInstance.archived_at || monitoringInstance.lifecycle_status === '已退役') return []

  if (monitoringInstance.monitoring_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (monitoringInstance.monitoring_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (monitoringInstance.monitoring_status === '暂停') {
    return [{ action: 'resume', label: '恢复监控' }]
  }

  return []
}

export function monitoringInstanceHealthGlyphState(monitoringInstance: MonitoringInstanceRecord): HealthState {
  if (monitoringInstance.monitoring_status === '维护中') return 'maintenance'
  if (monitoringInstance.monitoring_status === '暂停') return 'offline'
  if (monitoringInstance.current_health_status === '正常') return 'normal'
  if (monitoringInstance.current_health_status === '关注') return 'notice'
  if (monitoringInstance.current_health_status === '告警') return 'alert'
  if (monitoringInstance.current_health_status === '严重') return 'critical'
  return 'offline'
}

export function pauseConfirmationCurrent(monitoringInstance: MonitoringInstanceRecord) {
  return monitoringInstance.monitoring_status === '维护中'
    ? '当前：监控运行状态为维护中。'
    : '当前：监控运行状态为启用。'
}

export function mergeNonMetadataMonitoringInstanceRecord<T extends MonitoringInstanceRecord>(current: MonitoringInstanceRecord, updated: T): T {
  return {
    ...updated,
    group: current.group,
    labels: current.labels,
    note: current.note,
  }
}

export function applyOnboardingRecordToMonitoringInstance<T extends MonitoringInstanceOnboardingState>(
  current: MonitoringInstanceRecord | null,
  updated: T,
): T | MonitoringInstanceRecord {
  return current ? mergeNonMetadataMonitoringInstanceRecord(current, updated) : updated
}

export function formatAssetLocation(vps: VPSSummary): string {
  const parts = [vps.country, vps.region, vps.city].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '位置未确认'
}

export function isBindingConflictStatus(status: MonitoringInstanceRecord['binding_status']) {
  return status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
}

export function validateReturnVPSId(value: string | null | undefined): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed) return null
  if (!/^[a-zA-Z0-9._-]+$/.test(trimmed)) return null
  return trimmed
}

export function withReturnVPSQuery(path: string, value: string | null | undefined): string {
  const returnVPSId = validateReturnVPSId(value)
  if (!returnVPSId) return path
  const hashIndex = path.indexOf('#')
  const hash = hashIndex === -1 ? '' : path.slice(hashIndex)
  const withoutHash = hashIndex === -1 ? path : path.slice(0, hashIndex)
  const queryIndex = withoutHash.indexOf('?')
  const pathname = queryIndex === -1 ? withoutHash : withoutHash.slice(0, queryIndex)
  const params = new URLSearchParams(queryIndex === -1 ? '' : withoutHash.slice(queryIndex + 1))
  params.set('return_vps', returnVPSId)
  return `${pathname}?${params.toString()}${hash}`
}

export function returnVPSIdFromNavigationState(state: unknown): string | null {
  if (typeof state !== 'object' || state === null || !('return_vps' in state)) return null
  const value = Reflect.get(state, 'return_vps')
  return typeof value === 'string' ? validateReturnVPSId(value) : null
}

export function withReturnVPSNavigationState(locationState: unknown, returnVPSId: string | null): unknown {
  const base =
    typeof locationState === 'object' && locationState !== null && !Array.isArray(locationState)
      ? { ...(locationState as Record<string, unknown>) }
      : {}
  if (returnVPSId) base.return_vps = returnVPSId
  else delete base.return_vps
  return base
}
