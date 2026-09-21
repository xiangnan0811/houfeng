import type { BadgeTone, HealthState } from '../../components/atoms'
import { ApiError } from '../../lib/api'
import type { CreateTargetInput, TargetRecord } from '../../lib/types'
import type { CreateTargetFormState } from './types'

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

export function isCoverageGapTarget(target: TargetRecord) {
  return target.execution_monitoring_instance_labels.length === 0
}

const ABNORMAL_HEALTH: Record<string, BadgeTone> = {
  关注: 'notice',
  告警: 'alert',
  严重: 'critical',
}

export type TargetAttentionBadge = { label: string; tone: BadgeTone }

/** List attention cell: never 正常; control states occupy the cell. */
export function targetAttentionBadges(target: TargetRecord): TargetAttentionBadge[] {
  const badges: TargetAttentionBadge[] = []
  if (target.run_status === '维护中') {
    badges.push({ label: '维护中', tone: 'maintenance' })
  } else if (target.run_status === '暂停') {
    badges.push({ label: '暂停', tone: 'offline' })
  } else if (target.run_status === '已归档') {
    badges.push({ label: '已归档', tone: 'offline' })
  }
  const healthTone = ABNORMAL_HEALTH[target.current_health_status]
  if (healthTone) {
    badges.push({ label: target.current_health_status, tone: healthTone })
  } else if (badges.length === 0 && isCoverageGapTarget(target)) {
    badges.push({ label: '覆盖缺口', tone: 'notice' })
  }
  return badges
}

export function targetIssueSummary(target: TargetRecord): string {
  const summary = target.current_primary_issue_summary.trim()
  if (summary) return summary
  if (isCoverageGapTarget(target) && !ABNORMAL_HEALTH[target.current_health_status]) {
    return '缺少执行监控实例标签'
  }
  return ''
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

export function mergeMetadataTargetRecord(current: TargetRecord, updated: TargetRecord): TargetRecord {
  return {
    ...current,
    group: updated.group,
    labels: updated.labels,
    note: updated.note,
    updated_at: updated.updated_at,
  }
}
