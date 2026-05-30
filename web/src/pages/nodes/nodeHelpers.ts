import type { HealthState } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'
import type { CreateNodeInput, NodeRecord } from '../../lib/types'
import type { NodeEvidenceItem, NodeEvidenceLead, NodeFilterState, NodeRuntimeAction } from './types'

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

export function nodeEvidenceGlyphState(node: NodeRecord): HealthState {
  if (node.monitoring_status === '维护中' || node.monitoring_status === '暂停') {
    return nodeGlyphState(node)
  }
  if (isBindingConflictNode(node) || isPendingOnboardingNode(node)) return 'notice'
  return nodeGlyphState(node)
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
): Array<{ action: NodeRuntimeAction; label: string; variant?: 'danger' }> {
  const actions: Array<{ action: NodeRuntimeAction; label: string; variant?: 'danger' }> = []

  if (node.monitoring_status === '启用') {
    actions.push(
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停监控' },
    )
  } else if (node.monitoring_status === '维护中') {
    actions.push(
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停监控' },
    )
  } else if (node.monitoring_status === '暂停') {
    actions.push({ action: 'resume', label: '恢复监控' })
  }

  if (node.lifecycle_status === '在用' || node.lifecycle_status === '观察中' || node.lifecycle_status === '不续费') {
    actions.push({ action: 'retire', label: '退役', variant: 'danger' })
  } else if (node.lifecycle_status === '已退役') {
    actions.push({ action: 'restore-to-observing', label: '恢复观察' })
  }

  return actions
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

function nodeAttentionRank(node: NodeRecord): number {
  if (node.current_health_status === '严重') return 0
  if (node.current_health_status === '告警') return 1
  if (isBindingConflictNode(node)) return 2
  if (node.current_health_status === '关注') return 3
  if (isPendingOnboardingNode(node)) return 4
  if (node.monitoring_status === '维护中') return 5
  if (node.monitoring_status === '暂停') return 6
  return 9
}

function nodeEvidenceReason(node: NodeRecord): string {
  if (isBindingConflictNode(node)) return NODE_BINDING_CONFLICT_SUMMARY
  if (node.current_primary_issue_summary) return node.current_primary_issue_summary
  if (node.current_health_status !== '正常') return `健康状态：${node.current_health_status}`
  if (isPendingOnboardingNode(node)) return '接入 / 绑定未完成'
  if (node.monitoring_status === '维护中') return '维护窗口'
  if (node.monitoring_status === '暂停') return '监控暂停'
  return '当前没有明显异常'
}

function nodeEvidenceMeta(node: NodeRecord): string {
  const location = [node.group, node.region, node.city, node.provider].filter(Boolean).join(' · ')
  const freshness = node.last_heartbeat_at
    ? `心跳 ${formatDateTime(node.last_heartbeat_at)}`
    : '尚无心跳'
  const incident = `活跃异常 ${node.current_active_incident_count}`
  return [location || '未标记位置', freshness, incident].join(' · ')
}

export function pickTopNodeEvidence(nodes: NodeRecord[]): NodeEvidenceItem | null {
  if (nodes.length === 0) return null
  const candidates = [...nodes]
    .filter(
      (node) =>
        node.current_health_status !== '正常' ||
        isPendingOnboardingNode(node) ||
        node.monitoring_status === '维护中' ||
        node.monitoring_status === '暂停',
    )
    .sort((a, b) => {
      const rankDiff = nodeAttentionRank(a) - nodeAttentionRank(b)
      if (rankDiff !== 0) return rankDiff
      const incidentDiff = b.current_active_incident_count - a.current_active_incident_count
      if (incidentDiff !== 0) return incidentDiff
      return a.display_name.localeCompare(b.display_name, 'zh-Hans-CN')
    })

  const node = candidates[0]
  if (!node) return null

  return {
    node,
    title: node.display_name || node.node_id,
    reason: nodeEvidenceReason(node),
    meta: nodeEvidenceMeta(node),
    route: isPendingOnboardingNode(node) ? `/nodes/${node.node_id}?onboarding=1` : `/nodes/${node.node_id}`,
    actionLabel: isPendingOnboardingNode(node) ? '处理接入' : '查看证据',
  }
}

export function describeNodeFilterContext(filterState: NodeFilterState): string[] {
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

export function buildNodeEvidenceLead(args: {
  totalNodeCount: number
  displayedNodeCount: number
  abnormalNodeCount: number
  pendingOnboardingNodeCount: number
  maintenanceOrPausedNodeCount: number
  hasActiveFilters: boolean
}): NodeEvidenceLead {
  const {
    totalNodeCount,
    displayedNodeCount,
    abnormalNodeCount,
    pendingOnboardingNodeCount,
    maintenanceOrPausedNodeCount,
    hasActiveFilters,
  } = args

  if (displayedNodeCount === 0 && hasActiveFilters) {
    return {
      eyebrow: '当前筛选',
      title: '没有匹配当前证据条件',
      description: '调整筛选。',
      actionKind: 'clear',
      actionLabel: '清空证据筛选',
      tone: 'offline',
    }
  }

  if (totalNodeCount === 0) {
    return {
      eyebrow: '首次接入',
      title: '先建立第一条 Node 证据',
      description: '创建并接入。',
      actionKind: 'create',
      actionLabel: '建立 Node 证据',
      tone: 'notice',
    }
  }

  if (abnormalNodeCount > 0) {
    return {
      eyebrow: '优先证据',
      title: `先处理 ${abnormalNodeCount} 个异常节点`,
      description: '进详情处理。',
      actionKind: 'abnormal',
      actionLabel: '聚焦异常证据',
      tone: 'alert',
    }
  }

  if (pendingOnboardingNodeCount > 0) {
    return {
      eyebrow: '接入证据',
      title: `补齐 ${pendingOnboardingNodeCount} 个接入 / 绑定状态`,
      description: '未完成。',
      actionKind: 'onboarding',
      actionLabel: '聚焦接入证据',
      tone: 'notice',
    }
  }

  if (maintenanceOrPausedNodeCount > 0) {
    return {
      eyebrow: '运行上下文',
      title: `核对 ${maintenanceOrPausedNodeCount} 个维护 / 暂停节点`,
      description: '维护 / 暂停空窗。',
      actionKind: 'runtime',
      actionLabel: '聚焦运行证据',
      tone: 'maintenance',
    }
  }

  return {
    eyebrow: '证据稳定',
    title: 'Node 运行证据当前稳定',
    description: '无异常 / 接入缺口。',
    actionKind: 'asset',
    actionLabel: '查看 VPS 库存',
    tone: 'normal',
  }
}

export function isRuntimeAttentionNode(node: NodeRecord) {
  return node.monitoring_status === '维护中' || node.monitoring_status === '暂停'
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
