import type { BadgeTone } from '../../components/atoms'
import { ApiError } from '../../lib/api'
import type { CreateTargetInput, TargetRecord } from '../../lib/types'
import type { CreateTargetFormState } from './types'

const TARGET_TYPE_LABELS: Record<string, string> = {
  service: '服务',
  china_reference: '国内参考',
}

const UNKNOWN_TARGET_TYPE = '未知类型'

function mappedTargetType(value: string): string | undefined {
  if (!Object.hasOwn(TARGET_TYPE_LABELS, value)) return undefined
  const label = Object.getOwnPropertyDescriptor(TARGET_TYPE_LABELS, value)?.value
  return typeof label === 'string' && label ? label : undefined
}

/** User-facing target type. Wire values stay `service` / `china_reference`. */
export function targetTypePresentation(value: string): { label: string; raw: string | null } {
  const known = mappedTargetType(value)
  if (known) return { label: known, raw: null }
  const trimmed = value.trim()
  return { label: trimmed ? UNKNOWN_TARGET_TYPE : '', raw: trimmed || null }
}

export function targetTypeLabel(value: string): string {
  return targetTypePresentation(value).label
}

export const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: '服务' },
  { value: 'china_reference', label: '国内参考' },
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
] as const

export const TARGET_HEALTH_STATUS_FILTER_OPTIONS = [
  { value: '正常', label: '正常' },
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
  { value: '数据不可用', label: '数据不可用' },
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

const KNOWN_ABNORMAL_HEALTH = new Set(['关注', '告警', '严重'])

const ABNORMAL_HEALTH: Record<string, BadgeTone> = {
  关注: 'notice',
  告警: 'alert',
  严重: 'critical',
}

/** Retired targets stay in history. Archived-carrier visibility is a dashboard count, not a list-row inference. */
export function isCurrentListTarget(target: TargetRecord) {
  return target.lifecycle_status !== 'retired'
}

export function hasTargetObservation(target: TargetRecord) {
  return Boolean(target.last_success_at) || Boolean(target.last_failure_at)
}

export function isAbnormalTarget(target: TargetRecord) {
  return isCurrentListTarget(target)
    && target.run_status === '启用'
    && hasTargetObservation(target)
    && KNOWN_ABNORMAL_HEALTH.has(target.current_health_status)
}

export function isUnobservedTarget(target: TargetRecord) {
  return isCurrentListTarget(target)
    && target.run_status === '启用'
    && !target.last_success_at
    && !target.last_failure_at
}

/** Current, enabled, previously observed, and at least one stale probe. Pending without stale stays out. */
export function isStaleTarget(target: TargetRecord): boolean {
  return isCurrentListTarget(target)
    && target.run_status === '启用'
    && hasTargetObservation(target)
    && target.observation_freshness.stale_probe_count > 0
}

/** Dashboard normalizes a blank group to this name. The list query uses it unchanged. */
export const UNGROUPED_TARGET_GROUP = '未分组'

/** Exact group match. A blank or whitespace group also matches the dashboard name 未分组. */
export function targetMatchesGroup(target: TargetRecord, group: string): boolean {
  if (target.group === group) return true
  return group === UNGROUPED_TARGET_GROUP && target.group.trim() === ''
}

export function isTargetListInvalidatingError(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.status === 403 || error.status === 404)
}

function isAssignableTarget(target: TargetRecord) {
  return isCurrentListTarget(target) && (target.run_status === '启用' || target.run_status === '维护中')
}

export function isCoverageGapTarget(target: TargetRecord) {
  return isAssignableTarget(target) && (target.enabled_probe_count === 0 || target.matching_executor_count === 0)
}

export type TargetAttentionBadge = { label: string; tone: BadgeTone }

export function targetControlBadge(target: TargetRecord): TargetAttentionBadge | null {
  if (target.lifecycle_status === 'retired') return { label: '已退役', tone: 'offline' }
  if (target.run_status === '维护中') return { label: '维护中', tone: 'maintenance' }
  if (target.run_status === '暂停') return { label: '暂停', tone: 'offline' }
  return null
}

export function targetHealthBadge(target: TargetRecord): TargetAttentionBadge | null {
  if (target.lifecycle_status === 'retired') return null
  if (target.current_health_status === '数据不可用') return { label: '数据不可用', tone: 'neutral' }
  const tone = ABNORMAL_HEALTH[target.current_health_status]
  if (!tone) return null
  return { label: target.current_health_status, tone }
}

function showsFreshnessHint(target: TargetRecord): boolean {
  return target.lifecycle_status !== 'retired' && target.run_status === '启用'
}

/** Server freshness only. Inactive, paused, maintenance, and retired keep the control badge. */
export function targetFreshnessBadge(target: TargetRecord): TargetAttentionBadge | null {
  if (!showsFreshnessHint(target)) return null
  const freshness = target.observation_freshness
  switch (freshness.state) {
    case 'inactive':
      return null
    case 'fresh':
      return { label: '观测新鲜', tone: 'normal' }
    case 'pending':
      return { label: '等待新观测', tone: 'notice' }
    case 'partial':
      return freshness.stale_probe_count > 0
        ? { label: '部分观测过期', tone: 'alert' }
        : { label: '部分探测项等待观测', tone: 'notice' }
    case 'stale':
      return { label: '观测已过期', tone: 'alert' }
    case 'unobserved':
      return { label: '尚无观测', tone: 'neutral' }
    case 'uncovered':
      return { label: '未配置启用探测项', tone: 'notice' }
    default: {
      const unexpected: never = freshness.state
      return unexpected
    }
  }
}

/** Last-known health wording. Historical 正常 that is no longer fresh is not called 当前正常. */
export function targetKnownHealthNote(target: TargetRecord): string | null {
  if (!showsFreshnessHint(target) || target.observation_freshness.state === 'inactive') return null
  if (target.current_health_status === '正常' && target.observation_freshness.state !== 'fresh') {
    return '最近一次正常，当前证据不足'
  }
  if (KNOWN_ABNORMAL_HEALTH.has(target.current_health_status)) return '最近已知健康'
  return null
}

/** Control and health stay independent. 正常 stays quiet. Freshness is a separate hint. */
export function targetAttentionBadges(target: TargetRecord): TargetAttentionBadge[] {
  const control = targetControlBadge(target)
  const health = targetHealthBadge(target)
  const freshness = targetFreshnessBadge(target)
  if (target.lifecycle_status === 'retired') return control ? [control] : []
  return [control, health, freshness].filter((badge): badge is TargetAttentionBadge => badge !== null)
}

export function targetCoverageSummary(target: TargetRecord) {
  return `启用探测项 ${target.enabled_probe_count} · 可接收实例 ${target.matching_executor_count}`
}

export type TargetCoverageNotice = { key: string; title: string; detail: string }

export function targetCoverageNotices(target: TargetRecord): TargetCoverageNotice[] {
  if (target.lifecycle_status === 'retired') return []
  const notices: TargetCoverageNotice[] = []
  if (target.enabled_probe_count === 0) {
    notices.push({
      key: 'enabled-probes',
      title: '未配置启用探测项',
      detail: '当前启用探测项为 0。目标暂停后，已启用的探测项仍会计入。',
    })
  }
  if (isAssignableTarget(target) && target.matching_executor_count === 0) {
    notices.push({
      key: 'matching-executors',
      title: '没有可接收该任务的实例',
      detail: '按执行标签交集计算，并排除已归档、已退役和暂停的实例。',
    })
  }
  if (target.matching_executor_count > 0 && isUnobservedTarget(target)) {
    notices.push({
      key: 'unobserved-sample',
      title: '已匹配实例，尚无样本',
      detail: '可接收该任务的实例还没有产生成功或失败观测。',
    })
  }
  return notices
}

export function targetIssueSummary(target: TargetRecord): string {
  if (target.lifecycle_status === 'retired') return ''
  const summary = target.current_primary_issue_summary.trim()
  if (summary) return summary
  return targetCoverageNotices(target).map((notice) => notice.title).join('，')
}

export function countAbnormalTargets(targets: TargetRecord[]) {
  return targets.filter(isAbnormalTarget).length
}

export function countUnobservedTargets(targets: TargetRecord[]) {
  return targets.filter(isUnobservedTarget).length
}

export function countStaleTargets(targets: TargetRecord[]) {
  return targets.filter(isStaleTarget).length
}

export function countPausedTargets(targets: TargetRecord[]) {
  return targets.filter((target) => target.lifecycle_status !== 'retired' && target.run_status === '暂停').length
}

export function countArchivedTargets(targets: TargetRecord[]) {
  return targets.filter((target) => target.lifecycle_status === 'retired').length
}

/** Current wins on id overlap. Callers must pass the current and retired collections, not scope=all. */
export function combineCurrentAndRetiredTargets(
  currentTargets: TargetRecord[],
  retiredTargets: TargetRecord[],
) {
  const seen = new Set<string>()
  const combined: TargetRecord[] = []
  for (const target of [...currentTargets, ...retiredTargets]) {
    if (seen.has(target.target_id)) continue
    seen.add(target.target_id)
    combined.push(target)
  }
  return combined
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
