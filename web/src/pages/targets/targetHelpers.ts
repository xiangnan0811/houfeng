import type { HealthState } from '../../components/atoms'
import { ApiError } from '../../lib/api'
import type { CreateTargetInput, TargetRecord } from '../../lib/types'
import type { CreateTargetFormState, TargetRuntimeAction } from './types'

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
  executionNodeLabels: '',
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
  const executionNodeLabels = parseLabels(form.executionNodeLabels)
  if (executionNodeLabels.length === 0) {
    throw new Error('执行节点标签至少需要填写一个。')
  }

  const basePort = parseOptionalPositiveInteger(form.basePort, '基础端口')
  return {
    name: form.name.trim(),
    target_type: form.targetType,
    host: form.host.trim(),
    ...(basePort == null ? {} : { base_port: basePort }),
    execution_node_labels: executionNodeLabels,
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
