import type { RecordBusinessStatus, RecordDraftPayload, RecordSubjectReference, RecordType } from '../../lib/types'

const TYPE_STATUSES: Record<RecordType, readonly RecordBusinessStatus[]> = {
  troubleshooting: ['pending_investigation', 'investigating', 'verifying', 'resolved', 'closed', 'cancelled'],
  maintenance: ['planned', 'executing', 'verifying', 'completed', 'cancelled'],
  migration: ['planned', 'executing', 'verifying', 'completed', 'cancelled'],
  provider_communication: ['pending_contact', 'waiting_provider', 'waiting_internal', 'resolved', 'closed', 'cancelled'],
  billing: ['pending_review', 'processing', 'resolved', 'closed', 'cancelled'],
  important_finding: [],
  note: [],
}

const TYPE_DEFAULT_STATUS: Record<RecordType, RecordBusinessStatus | ''> = {
  troubleshooting: 'pending_investigation',
  maintenance: 'planned',
  migration: 'planned',
  provider_communication: 'pending_contact',
  billing: 'pending_review',
  important_finding: '',
  note: '',
}

export const BUSINESS_STATUS_LABELS: Record<RecordBusinessStatus, string> = {
  pending_investigation: '待排查',
  investigating: '排查中',
  verifying: '验证中',
  resolved: '已解决',
  closed: '已关闭',
  cancelled: '已取消',
  planned: '已计划',
  executing: '执行中',
  completed: '已完成',
  pending_contact: '待联系',
  waiting_provider: '等待服务商',
  waiting_internal: '等待内部',
  pending_review: '待复核',
  processing: '处理中',
}

const TYPE_TEMPLATES: Record<RecordType, string> = {
  troubleshooting: '## 现象\n\n## 排查\n\n## 结论\n',
  maintenance: '## 计划\n\n## 执行\n\n## 验证\n',
  migration: '## 范围\n\n## 步骤\n\n## 回滚\n',
  provider_communication: '## 诉求\n\n## 往来\n\n## 结论\n',
  billing: '## 账单事实\n\n## 处理\n\n## 结论\n',
  important_finding: '## 发现\n\n## 影响\n\n## 后续\n',
  note: '## 备忘\n',
}

export function defaultBusinessStatus(recordType: RecordType): RecordBusinessStatus | '' {
  return TYPE_DEFAULT_STATUS[recordType]
}

export function businessStatusesForType(recordType: RecordType): readonly RecordBusinessStatus[] {
  return TYPE_STATUSES[recordType]
}

export function typeSupportsBusinessStatus(recordType: RecordType): boolean {
  return TYPE_STATUSES[recordType].length > 0
}

export function applyRecordTypeChange(
  recordType: RecordType,
): Pick<RecordDraftPayload, 'record_type' | 'business_status'> {
  return {
    record_type: recordType,
    business_status: defaultBusinessStatus(recordType),
  }
}

export function patchPrimarySubject(
  subjects: readonly RecordSubjectReference[],
  sourceId: string,
): RecordSubjectReference[] {
  if (subjects.length === 0) {
    return [{
      registry_version: 1,
      kind: 'vps',
      role: 'affected',
      source_id: sourceId,
      primary: true,
    }]
  }
  let replaced = false
  const next = subjects.map((subject) => {
    if (!subject.primary) return subject
    replaced = true
    return { ...subject, source_id: sourceId }
  })
  if (replaced) return next
  const [first, ...rest] = subjects
  if (!first) {
    return [{
      registry_version: 1,
      kind: 'vps',
      role: 'affected',
      source_id: sourceId,
      primary: true,
    }]
  }
  return [{ ...first, source_id: sourceId, primary: true }, ...rest]
}

export function insertMarkdownAroundSelection(
  source: string,
  start: number,
  end: number,
  before: string,
  after = before,
): { value: string; selectionStart: number; selectionEnd: number } {
  const from = Math.max(0, Math.min(start, end, source.length))
  const to = Math.max(from, Math.min(Math.max(start, end), source.length))
  const selected = source.slice(from, to)
  return {
    value: `${source.slice(0, from)}${before}${selected}${after}${source.slice(to)}`,
    selectionStart: from + before.length,
    selectionEnd: from + before.length + selected.length,
  }
}

export function insertMarkdownSnippet(source: string, snippet: string): string {
  const trimmed = snippet.trim()
  if (trimmed.length === 0) return source
  if (source.includes(trimmed)) return source
  return source.trim().length === 0 ? trimmed : `${source.trimEnd()}\n\n${trimmed}`
}

export function templateMarkdownForType(recordType: RecordType): string {
  return TYPE_TEMPLATES[recordType]
}
