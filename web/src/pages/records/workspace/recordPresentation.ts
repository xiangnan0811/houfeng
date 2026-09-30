import type { BadgeTone } from '../../../components/atoms'
import type { RecordDraftPayload, RecordRevision, RecordStatusGroup, RecordSubject, RecordSubjectReference } from '../../../lib/types'
import { RECORD_SUBJECT_KIND_LABELS } from '../recordLabels'

const STATUS_GROUP_TONES: Record<RecordStatusGroup, BadgeTone> = {
  pending: 'notice',
  in_progress: 'notice',
  waiting: 'maintenance',
  verification: 'notice',
  completed: 'normal',
  cancelled: 'neutral',
}

export function statusGroupTone(group: RecordStatusGroup | undefined): BadgeTone {
  return group ? STATUS_GROUP_TONES[group] : 'neutral'
}

// 影响级别是自由文本；只把常见的高等级词映射成告警色，其余保持中性。
export function impactTone(level: string): BadgeTone {
  const normalized = level.trim().toLowerCase()
  if (normalized === 'critical' || normalized === 'p0' || normalized === '严重') return 'critical'
  if (normalized === 'high' || normalized === 'p1' || normalized === '高') return 'alert'
  return 'neutral'
}

function hasIdentity(subject: RecordSubjectReference | RecordSubject): subject is RecordSubject {
  return 'identity' in subject && typeof subject.identity?.display_name === 'string'
}

/** 主体的可读名称：优先服务端身份名，缺失时退回类型 + 源 ID。 */
export function subjectLabel(subject: RecordSubjectReference | RecordSubject | undefined): string {
  if (!subject) return ''
  const kind = RECORD_SUBJECT_KIND_LABELS[subject.kind]
  const name = hasIdentity(subject) ? subject.identity.display_name.trim() : ''
  return name ? `${kind} · ${name}` : `${kind} · ${subject.source_id}`
}

export function primarySubject<T extends RecordSubjectReference>(subjects: readonly T[]): T | undefined {
  return subjects.find((subject) => subject.primary) ?? subjects[0]
}

/** 阅读态展示用：已发布修订带有身份信息时优先用它，否则用本地载荷。 */
export function displaySubject(
  revision: RecordRevision | null | undefined,
  payload: RecordDraftPayload,
): RecordSubjectReference | RecordSubject | undefined {
  return primarySubject(revision?.subjects ?? []) ?? primarySubject(payload.subjects)
}

/** 计数徽标：为零时保持中性，大于零才使用强调色。 */
export function countClass(count: number): string {
  return count > 0 ? 'record-count record-count--active' : 'record-count'
}
