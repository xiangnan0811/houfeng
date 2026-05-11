import type { HealthState } from '../../components/atoms'
import type { CreateNodeInput, NodeRecord } from '../../lib/types'
import type { NodeRuntimeAction } from './types'

export const NODE_LIFECYCLE_FILTER_OPTIONS = [
  { value: '待接入', label: '待接入' },
  { value: '在用', label: '在用' },
  { value: '观察中', label: '观察中' },
  { value: '不续费', label: '不续费' },
  { value: '已退役', label: '已退役' },
] as const

export const NODE_RUN_STATUS_FILTER_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '暂停', label: '暂停' },
  { value: '维护中', label: '维护中' },
] as const

export const NODE_HEALTH_STATUS_FILTER_OPTIONS = [
  { value: '正常', label: '正常' },
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
] as const

export const initialCreateForm: CreateNodeInput = {
  display_name: '',
  group: '',
  region: '',
  city: '',
  provider: '',
  labels: [],
  note: '',
}

export const NODE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
export const NODE_BINDING_UNBOUND_STATUS = '未绑定'
export const NODE_BINDING_CONFLICT_SUMMARY = '等待绑定确认'

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
 *  Health is derived (正常/关注/告警/严重) per architecture-data-model §node;
 *  monitoring 维护中/暂停 visually outranks health for at-a-glance scanning. */
export function nodeGlyphState(node: NodeRecord): HealthState {
  if (node.monitoring_status === '维护中') return 'maintenance'
  if (node.monitoring_status === '暂停') return 'offline'
  switch (node.current_health_status) {
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

export function nodeRuntimeActions(
  node: NodeRecord,
): Array<{ action: NodeRuntimeAction; label: string }> {
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

export function isBindingConflictNode(node: NodeRecord) {
  return node.binding_status === NODE_BINDING_CONFLICT_STATUS
}

export function isPendingOnboardingNode(node: NodeRecord) {
  return (
    node.lifecycle_status === '待接入' ||
    node.binding_status === NODE_BINDING_UNBOUND_STATUS ||
    node.binding_status === NODE_BINDING_CONFLICT_STATUS
  )
}

export function countAbnormalNodes(nodes: NodeRecord[]) {
  return nodes.filter((node) => node.current_health_status !== '正常').length
}

export function countPendingOnboardingNodes(nodes: NodeRecord[]) {
  return nodes.filter(isPendingOnboardingNode).length
}

export function countMaintenanceOrPausedNodes(nodes: NodeRecord[]) {
  return nodes.filter(
    (node) => node.monitoring_status === '维护中' || node.monitoring_status === '暂停',
  ).length
}

export function runtimeAttentionFilter(nodes: NodeRecord[]): string | null {
  if (nodes.some((node) => node.monitoring_status === '维护中')) return '维护中'
  if (nodes.some((node) => node.monitoring_status === '暂停')) return '暂停'
  return null
}

export function pauseConfirmationCurrent(node: NodeRecord) {
  return node.monitoring_status === '维护中'
    ? '当前：监控运行状态为维护中。'
    : '当前：监控运行状态为启用。'
}

export function mergeNonMetadataNodeRecord(current: NodeRecord, updated: NodeRecord): NodeRecord {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

export function actionButtonKey(nodeId: string, action: NodeRuntimeAction) {
  return `${nodeId}:${action}`
}
