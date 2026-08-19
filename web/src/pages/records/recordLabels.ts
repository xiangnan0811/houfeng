import type {
  RecordActionState,
  RecordFollowUpState,
  RecordLifecycle,
  RecordRelationRole,
  RecordSort,
  RecordStatusGroup,
  RecordSubjectKind,
  RecordSubjectPlacement,
  RecordType,
} from '../../lib/types'

/**
 * Chinese labels for the closed record vocabularies. Each map is exhaustive over
 * its union, so it doubles as the list of values the UI may offer and the search
 * filter model may accept — the two cannot drift apart.
 *
 * Business status labels stay in `recordWorkspaceModel` next to the
 * type-to-status rules that decide which of them apply.
 */
export const RECORD_TYPE_LABELS: Record<RecordType, string> = {
  note: '备忘',
  troubleshooting: '排障',
  maintenance: '维护',
  migration: '迁移',
  provider_communication: '服务商沟通',
  billing: '账单',
  important_finding: '重要发现',
}

export const RECORD_STATUS_GROUP_LABELS: Record<RecordStatusGroup, string> = {
  pending: '待处理',
  in_progress: '进行中',
  waiting: '等待中',
  verification: '验证中',
  completed: '已完成',
  cancelled: '已取消',
}

export const RECORD_LIFECYCLE_LABELS: Record<RecordLifecycle, string> = {
  active: '在用',
  archived: '已归档',
}

export const RECORD_FOLLOW_UP_LABELS: Record<RecordFollowUpState, string> = {
  none: '无跟进时间',
  scheduled: '已排期',
  overdue: '已逾期',
}

export const RECORD_ACTION_LABELS: Record<RecordActionState, string> = {
  none: '无待办',
  open: '有待办',
  overdue: '待办逾期',
}

export const RECORD_SUBJECT_KIND_LABELS: Record<RecordSubjectKind, string> = {
  vps: 'VPS',
  monitoring_instance: '监控实例',
  target: '探测目标',
}

export const RECORD_RELATION_ROLE_LABELS: Record<RecordRelationRole, string> = {
  affected: '受影响',
  context: '上下文',
  evidence_source: '证据来源',
}

export const RECORD_SUBJECT_PLACEMENT_LABELS: Record<RecordSubjectPlacement, string> = {
  primary: '主对象',
  related: '关联对象',
}

export const RECORD_SORT_LABELS: Record<RecordSort, string> = {
  updated_at_desc: '最近更新在前',
  updated_at_asc: '最早更新在前',
}

/** Options for a `<Select>`, in the order the labels are declared. */
export function labelOptions<Value extends string>(
  labels: Record<Value, string>,
): { value: Value, label: string }[] {
  return (Object.keys(labels) as Value[]).map((value) => ({ value, label: labels[value] }))
}

export function labelVocabulary<Value extends string>(
  labels: Record<Value, string>,
): ReadonlySet<Value> {
  return new Set(Object.keys(labels) as Value[])
}
