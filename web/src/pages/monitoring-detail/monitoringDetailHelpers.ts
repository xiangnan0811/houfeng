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
  requestedActivityMonitoringInstanceId: null,
  incidents: [],
  incidentsError: null,
  events: [],
  eventsError: null,
}

export function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
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

export function monitoringInstanceRuntimeActions(monitoringInstance: MonitoringInstanceRecord): Array<{ action: MonitoringInstanceRuntimeAction; label: string }> {
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
