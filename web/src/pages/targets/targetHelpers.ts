import type { HealthState } from '../../components/atoms'
import { ApiError } from '../../lib/api'
import { formatDateTime } from '../../lib/format'
import type { CreateTargetInput, TargetRecord } from '../../lib/types'
import type {
  CreateTargetFormState,
  TargetEvidenceItem,
  TargetEvidenceLead,
  TargetFilterState,
  TargetRuntimeAction,
} from './types'

export const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

export const TARGET_RUN_STATUS_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '维护中', label: '维护中' },
  { value: '暂停', label: '暂停' },
] as const

export const TARGET_RUN_STATUS_FILTER_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '维护中', label: '维护中' },
  { value: '暂停', label: '暂停' },
  { value: '已归档', label: '已归档' },
] as const

export const TARGET_HEALTH_STATUS_FILTER_OPTIONS = [
  { value: '正常', label: '正常' },
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
] as const

export const initialCreateForm: CreateTargetFormState = {
  name: '',
  targetType: 'service',
  host: '',
  basePort: '',
  executionMonitoringInstanceLabels: '',
  runStatus: '启用',
  group: '',
  labels: '',
  note: '',
}

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
    if (!seen.has(value)) {
      seen.add(value)
      out.push(value)
    }
  }
  return out.sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

/** Map target run_status + health into the StatusGlyph state vocabulary.
 *  v1 baseline: maintenance / 暂停 / 已归档 outrank health for at-a-glance scanning. */
export function targetGlyphState(target: TargetRecord): HealthState {
  if (target.run_status === '已归档') return 'offline'
  if (target.run_status === '维护中') return 'maintenance'
  if (target.run_status === '暂停') return 'offline'
  switch (target.current_health_status) {
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

export function targetEvidenceGlyphState(target: TargetRecord): HealthState {
  const state = targetGlyphState(target)
  if (state !== 'normal') return state
  if (isCoverageGapTarget(target)) return 'notice'
  return state
}

export function isCoverageGapTarget(target: TargetRecord) {
  return target.execution_monitoring_instance_labels.length === 0
}

export function countAbnormalTargets(targets: TargetRecord[]) {
  return targets.filter((target) => target.current_health_status !== '正常').length
}

export function countPausedTargets(targets: TargetRecord[]) {
  return targets.filter((target) => target.run_status === '暂停').length
}

export function countArchivedTargets(targets: TargetRecord[]) {
  return targets.filter((target) => target.run_status === '已归档').length
}

export function countCoverageGapTargets(targets: TargetRecord[]) {
  return targets.filter(isCoverageGapTarget).length
}

function targetAttentionRank(target: TargetRecord): number {
  if (target.current_health_status === '严重') return 0
  if (target.current_health_status === '告警') return 1
  if (target.current_health_status === '关注') return 2
  if (target.run_status === '暂停') return 3
  if (target.run_status === '已归档') return 4
  if (target.run_status === '维护中') return 5
  if (isCoverageGapTarget(target)) return 6
  return 9
}

function targetEvidenceReason(target: TargetRecord): string {
  if (target.current_primary_issue_summary) return target.current_primary_issue_summary
  if (target.current_health_status !== '正常') return `健康状态：${target.current_health_status}`
  if (target.run_status === '暂停') return '目标暂停，当前不会产生新的入口探测'
  if (target.run_status === '已归档') return '目标已归档，不再作为活跃入口证据'
  if (target.run_status === '维护中') return '维护窗口内，探测空窗应按维护上下文解读'
  if (isCoverageGapTarget(target)) return '缺少执行监控实例标签，探测覆盖边界不明确'
  return '当前没有明显异常'
}

function targetEvidenceMeta(target: TargetRecord): string {
  const host = target.base_port ? `${target.host}:${target.base_port}` : target.host
  const group = target.group || '未分组'
  const coverage =
    target.execution_monitoring_instance_labels.length > 0
      ? `执行 ${target.execution_monitoring_instance_labels.join(' · ')}`
      : '无执行标签'
  const freshness = target.last_failure_at
    ? `失败 ${formatDateTime(target.last_failure_at)}`
    : target.last_success_at
      ? `成功 ${formatDateTime(target.last_success_at)}`
      : '尚无观测'
  const incident = `活跃异常 ${target.current_active_incident_count}`
  return [host, group, coverage, freshness, incident].join(' · ')
}

export function pickTopTargetEvidence(targets: TargetRecord[]): TargetEvidenceItem | null {
  if (targets.length === 0) return null
  const candidates = [...targets]
    .filter(
      (target) =>
        target.current_health_status !== '正常' ||
        target.run_status === '暂停' ||
        target.run_status === '已归档' ||
        target.run_status === '维护中' ||
        isCoverageGapTarget(target),
    )
    .sort((a, b) => {
      const rankDiff = targetAttentionRank(a) - targetAttentionRank(b)
      if (rankDiff !== 0) return rankDiff
      const incidentDiff = b.current_active_incident_count - a.current_active_incident_count
      if (incidentDiff !== 0) return incidentDiff
      return a.name.localeCompare(b.name, 'zh-Hans-CN')
    })

  const target = candidates[0]
  if (!target) return null

  return {
    target,
    title: target.name || target.target_id,
    reason: targetEvidenceReason(target),
    meta: targetEvidenceMeta(target),
    route: `/targets/${target.target_id}`,
    actionLabel: '查看入口证据',
  }
}

export function describeTargetFilterContext(filterState: TargetFilterState): string[] {
  const items: string[] = []
  if (filterState.group) items.push(`Group ${filterState.group}`)
  if (filterState.type) items.push(`类型 ${filterState.type}`)
  if (filterState.runStatus) items.push(`运行 ${filterState.runStatus}`)
  if (filterState.health) items.push(`健康 ${filterState.health}`)
  for (const label of filterState.labels) items.push(`标签 ${label}`)
  for (const label of filterState.executionLabels) items.push(`执行监控实例标签 ${label}`)
  if (filterState.abnormal) items.push('仅看异常')
  return items
}

export function buildTargetEvidenceLead(args: {
  totalTargetCount: number
  displayedTargetCount: number
  abnormalTargetCount: number
  pausedTargetCount: number
  archivedTargetCount: number
  coverageGapTargetCount: number
  hasActiveFilters: boolean
}): TargetEvidenceLead {
  const {
    totalTargetCount,
    displayedTargetCount,
    abnormalTargetCount,
    pausedTargetCount,
    archivedTargetCount,
    coverageGapTargetCount,
    hasActiveFilters,
  } = args

  if (displayedTargetCount === 0 && hasActiveFilters) {
    return {
      eyebrow: '当前筛选',
      title: '没有匹配当前入口证据',
      description: '当前筛选没有返回 Target。先清空或收窄条件，再继续判断服务入口证据。',
      actionKind: 'clear',
      actionLabel: '清空入口筛选',
      tone: 'offline',
    }
  }

  if (totalTargetCount === 0) {
    return {
      eyebrow: '首次入口',
      title: '先建立第一个 Target 入口',
      description: '还没有服务入口可用于探测覆盖和资产判断。创建 Target 后再配置 ProbeItem。',
      actionKind: 'create',
      actionLabel: '建立 Target 入口',
      tone: 'notice',
    }
  }

  if (abnormalTargetCount > 0) {
    return {
      eyebrow: '优先入口',
      title: `先处理 ${abnormalTargetCount} 个异常入口`,
      description: '异常 Target 会影响服务入口可用性判断，优先进入目标详情或事件时间线核对。',
      actionKind: 'abnormal',
      actionLabel: '聚焦异常入口',
      tone: 'alert',
    }
  }

  const inactiveCount = pausedTargetCount + archivedTargetCount
  if (inactiveCount > 0) {
    return {
      eyebrow: '运行上下文',
      title: `核对 ${inactiveCount} 个暂停 / 归档入口`,
      description: '暂停和归档会解释观测缺口，避免把主动下线误判成服务入口故障。',
      actionKind: pausedTargetCount > 0 ? 'paused' : 'archived',
      actionLabel: pausedTargetCount > 0 ? '聚焦暂停入口' : '聚焦归档入口',
      tone: 'maintenance',
    }
  }

  if (coverageGapTargetCount > 0) {
    return {
      eyebrow: '覆盖边界',
      title: `补齐 ${coverageGapTargetCount} 个执行覆盖标签`,
      description: '这些 Target 缺少执行监控实例标签，探测从哪里发出的边界不明确。',
      actionKind: 'coverage',
      actionLabel: '核对监控实例覆盖',
      tone: 'notice',
    }
  }

  return {
    eyebrow: '证据稳定',
    title: 'Target 入口证据当前稳定',
    description: '当前没有异常入口、暂停归档对象或执行覆盖缺口。下一步可回到资产决策队列核对资产侧问题。',
    actionKind: 'asset',
    actionLabel: '查看资产决策',
    tone: 'normal',
  }
}

export function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function parseLabels(value: string) {
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function dedupeLabels(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index)
}

function parseOptionalPositiveInteger(value: string, label: string): number | undefined {
  const normalized = value.trim()
  if (normalized === '') return undefined
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error(`${label}必须为正整数。`)
  }
  return Number.parseInt(normalized, 10)
}

export function buildCreateTargetInput(form: CreateTargetFormState): CreateTargetInput {
  const executionMonitoringInstanceLabels = parseLabels(form.executionMonitoringInstanceLabels)
  if (executionMonitoringInstanceLabels.length === 0) {
    throw new Error('执行监控实例标签至少需要填写一个。')
  }

  const basePort = parseOptionalPositiveInteger(form.basePort, '基础端口')
  return {
    name: form.name.trim(),
    target_type: form.targetType,
    host: form.host.trim(),
    ...(basePort == null ? {} : { base_port: basePort }),
    execution_monitoring_instance_labels: executionMonitoringInstanceLabels,
    run_status: form.runStatus,
    group: form.group.trim(),
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

export function targetRuntimeActions(
  target: TargetRecord,
): Array<{ action: TargetRuntimeAction; label: string }> {
  if (target.run_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '暂停') {
    return [
      { action: 'resume', label: '恢复' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '已归档') {
    return [{ action: 'restore-to-paused', label: '恢复到暂停' }]
  }

  return []
}

export function pauseConfirmationCurrent() {
  return '当前：目标运行状态为启用或维护中。'
}

export function actionButtonKey(targetId: string, action: TargetRuntimeAction) {
  return `${targetId}:${action}`
}

export function focusRestoreActionAfterSuccess(action: TargetRuntimeAction): TargetRuntimeAction {
  switch (action) {
    case 'enter-maintenance':
      return 'exit-maintenance'
    case 'exit-maintenance':
      return 'enter-maintenance'
    case 'pause':
      return 'resume'
    case 'resume':
      return 'pause'
    case 'archive':
      return 'restore-to-paused'
    case 'restore-to-paused':
      return 'resume'
  }
}

export function mergeRuntimeTargetRecord(current: TargetRecord, updated: TargetRecord): TargetRecord {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

export function mergeMetadataTargetRecord(current: TargetRecord, updated: TargetRecord): TargetRecord {
  return {
    ...current,
    group: updated.group,
    labels: updated.labels,
    note: updated.note,
    updated_at: updated.updated_at,
  }
}
