import type { HealthState } from '../../components/atoms'
import type { NodeRuntimeAction } from '../../components/node-detail'
import { ApiError } from '../../lib/api'
import type {
  NodeOnboardingState,
  NodeRecord,
  PendingBindingMetadata,
  VPSSummary,
} from '../../lib/types'
import { NODE_BINDING_CONFLICT_STATUS } from './nodeDetailConstants'
import type { NodeDetailPageState } from './types'

export const INITIAL_NODE_DETAIL_STATE: NodeDetailPageState = {
  requestedNodeId: null,
  error: null,
  node: null,
  runtimeFacts: null,
  requestedActivityNodeId: null,
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

export function currentFingerprintSummary(onboarding: NodeOnboardingState | null) {
  if (onboarding?.current_binding_fingerprint_summary?.trim()) {
    return onboarding.current_binding_fingerprint_summary.trim()
  }
  return '服务端当前未提供已绑定指纹摘要'
}

export function pendingBindingMetadata(onboarding: NodeOnboardingState | null): PendingBindingMetadata | null {
  return onboarding?.pending_binding ?? null
}

export function nodeRuntimeActions(node: NodeRecord): Array<{ action: NodeRuntimeAction; label: string }> {
  if (node.monitoring_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (node.monitoring_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (node.monitoring_status === '暂停') {
    return [{ action: 'resume', label: '恢复监控' }]
  }

  return []
}

export function nodeHealthGlyphState(node: NodeRecord): HealthState {
  if (node.monitoring_status === '维护中') return 'maintenance'
  if (node.monitoring_status === '暂停') return 'offline'
  if (node.current_health_status === '正常') return 'normal'
  if (node.current_health_status === '关注') return 'notice'
  if (node.current_health_status === '告警') return 'alert'
  if (node.current_health_status === '严重') return 'critical'
  return 'offline'
}

export function pauseConfirmationCurrent(node: NodeRecord) {
  return node.monitoring_status === '维护中'
    ? '当前：监控运行状态为维护中。'
    : '当前：监控运行状态为启用。'
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

export function mergeNonMetadataNodeRecord<T extends NodeRecord>(current: NodeRecord, updated: T): T {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

export function applyOnboardingRecordToNode<T extends NodeOnboardingState>(
  current: NodeRecord | null,
  updated: T,
): T | NodeRecord {
  return current ? mergeNonMetadataNodeRecord(current, updated) : updated
}

export function formatAssetLocation(vps: VPSSummary): string {
  const parts = [vps.country, vps.region, vps.city].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '位置未确认'
}

export function isBindingConflictStatus(status: NodeRecord['binding_status']) {
  return status === NODE_BINDING_CONFLICT_STATUS
}
