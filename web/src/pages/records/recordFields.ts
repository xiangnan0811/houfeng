import type { RecordDraftPayload, RecordRevision } from '../../lib/types'
import { payloadFromRevision } from './recordPayload'

/**
 * The comparable fields of a record revision. Diff and conflict resolution both read
 * this list so a field can never be shown in one place and silently dropped in the
 * other; `markdown_dialect_version` is excluded because it is a contract constant.
 */
export type RecordComparableField = Exclude<keyof RecordDraftPayload, 'markdown_dialect_version'>

export const RECORD_COMPARABLE_FIELDS: readonly { field: RecordComparableField; label: string }[] = [
  { field: 'title', label: '标题' },
  { field: 'record_type', label: '类型' },
  { field: 'business_status', label: '业务状态' },
  { field: 'impact_level', label: '影响级别' },
  { field: 'occurred_at', label: '发生时间' },
  { field: 'completed_at', label: '完成时间' },
  { field: 'owner_id', label: '负责人' },
  { field: 'participant_ids', label: '参与者' },
  { field: 'follow_up_at', label: '跟进时间' },
  { field: 'visibility', label: '可见范围' },
  { field: 'subjects', label: '关联对象' },
  { field: 'tags', label: '标签' },
  { field: 'attachment_ids', label: '附件' },
  { field: 'template', label: '模板' },
  { field: 'save_reason', label: '保存原因' },
  { field: 'body_markdown', label: '正文' },
]

export function recordComparablePayload(value: RecordDraftPayload | RecordRevision): RecordDraftPayload {
  if ('markdown_dialect_version' in value && 'participant_ids' in value) return value
  return payloadFromRevision(value)
}

export function recordFieldText(payload: RecordDraftPayload, field: RecordComparableField): string {
  const value = payload[field]
  if (value === null || value === undefined || value === '') return ''
  if (Array.isArray(value)) return value.map(scalarText).join('、')
  if (typeof value === 'object') return scalarText(value)
  return String(value)
}

export function differingRecordFields(
  left: RecordDraftPayload,
  right: RecordDraftPayload,
): readonly { field: RecordComparableField; label: string }[] {
  return RECORD_COMPARABLE_FIELDS.filter(
    ({ field }) => recordFieldText(left, field) !== recordFieldText(right, field),
  )
}

export function mergeRecordFields(
  local: RecordDraftPayload,
  server: RecordDraftPayload,
  serverFields: readonly RecordComparableField[],
): RecordDraftPayload {
  const merged: RecordDraftPayload = { ...local }
  for (const field of serverFields) {
    Object.assign(merged, { [field]: server[field] })
  }
  return merged
}

function scalarText(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value !== 'object') return String(value)
  const record = value as Record<string, unknown>
  for (const key of ['source_id', 'participant_id', 'id', 'kind']) {
    const candidate = record[key]
    if (typeof candidate === 'string' && candidate !== '') {
      return key === 'kind' ? summarizeObject(record) : candidate
    }
  }
  return summarizeObject(record)
}

function summarizeObject(record: Record<string, unknown>): string {
  return Object.entries(record)
    .filter(([, entry]) => entry !== null && entry !== undefined && entry !== '')
    .map(([key, entry]) => `${key}=${Array.isArray(entry) ? entry.join('/') : String(entry)}`)
    .sort()
    .join(' ')
}
