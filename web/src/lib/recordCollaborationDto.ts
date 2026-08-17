import type {
  RecordAction,
  RecordActionListResponse,
  RecordActionMutation,
  RecordComment,
  RecordCommentListResponse,
  RecordCommentMutation,
  RecordNotification,
  RecordNotificationListResponse,
  RecordNotificationTarget,
  RecordWatch,
} from './types'

const invalidResponse = 'invalid_record_collaboration_response'

function invalid(): never {
  throw new Error(invalidResponse)
}

function objectValue(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) invalid()
  return value as Record<string, unknown>
}

function text(value: unknown): string {
  if (typeof value !== 'string') invalid()
  return value
}

function nullableText(value: unknown): string | null {
  if (value === null) return null
  return text(value)
}

function integer(value: unknown): number {
  if (!Number.isSafeInteger(value) || (value as number) < 0) invalid()
  return value as number
}

function booleanValue(value: unknown): boolean {
  if (typeof value !== 'boolean') invalid()
  return value
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) invalid()
  return value.map(text)
}

function enumValue<const T extends string>(value: unknown, values: readonly T[]): T {
  if (typeof value !== 'string' || !values.includes(value as T)) invalid()
  return value as T
}

export function decodeRecordAction(value: unknown): RecordAction {
  const item = objectValue(value)
  return {
    action_id: text(item.action_id),
    record_id: text(item.record_id),
    version: integer(item.version),
    status: enumValue(item.status, ['open', 'completed', 'cancelled']),
    title: text(item.title),
    assignee_id: text(item.assignee_id),
    due_at: nullableText(item.due_at),
    completed_at: nullableText(item.completed_at),
    subject_revision_id: text(item.subject_revision_id),
    created_at: text(item.created_at),
    updated_at: text(item.updated_at),
  }
}

export function decodeRecordActionList(value: unknown): RecordActionListResponse {
  const response = objectValue(value)
  if (!Array.isArray(response.items)) invalid()
  return { items: response.items.map(decodeRecordAction) }
}

export function decodeRecordActionMutation(value: unknown): RecordActionMutation {
  const item = objectValue(value)
  return {
    action_id: text(item.action_id),
    record_id: text(item.record_id),
    version: integer(item.version),
    status: enumValue(item.status, ['open', 'completed', 'cancelled']),
    event_kind: enumValue(item.event_kind, ['created', 'updated', 'completed', 'cancelled', 'reopened']),
    replayed: booleanValue(item.replayed),
    changed_at: text(item.changed_at),
  }
}

export function decodeRecordComment(value: unknown): RecordComment {
  const item = objectValue(value)
  return {
    comment_id: text(item.comment_id),
    record_id: text(item.record_id),
    author_id: text(item.author_id),
    version: integer(item.version),
    state: enumValue(item.state, ['active', 'redacted']),
    body_markdown: nullableText(item.body_markdown),
    render_model: item.render_model ?? null,
    reply_to_comment_id: text(item.reply_to_comment_id),
    mention_user_ids: stringArray(item.mention_user_ids),
    created_at: text(item.created_at),
    updated_at: text(item.updated_at),
    redacted_at: nullableText(item.redacted_at),
  }
}

export function decodeRecordCommentList(value: unknown): RecordCommentListResponse {
  const response = objectValue(value)
  if (!Array.isArray(response.comments)) invalid()
  return { comments: response.comments.map(decodeRecordComment) }
}

export function decodeRecordCommentMutation(value: unknown): RecordCommentMutation {
  const item = objectValue(value)
  return {
    comment_id: text(item.comment_id),
    record_id: text(item.record_id),
    version: integer(item.version),
    state: enumValue(item.state, ['active', 'redacted']),
    event_kind: enumValue(item.event_kind, ['created', 'edited', 'redacted']),
    replayed: booleanValue(item.replayed),
    changed_at: text(item.changed_at),
  }
}

export function decodeRecordWatch(value: unknown): RecordWatch {
  const item = objectValue(value)
  const sources = objectValue(item.sources)
  return {
    record_id: text(item.record_id),
    user_id: text(item.user_id),
    version: integer(item.version),
    preference: enumValue(item.preference, ['default', 'watching', 'muted']),
    sources: {
      author: booleanValue(sources.author),
      owner: booleanValue(sources.owner),
      participant: booleanValue(sources.participant),
      comment: booleanValue(sources.comment),
      mention: booleanValue(sources.mention),
      action: booleanValue(sources.action),
    },
    updated_at: nullableText(item.updated_at),
  }
}

export function decodeRecordNotification(value: unknown): RecordNotification {
  const item = objectValue(value)
  return {
    notification_id: text(item.notification_id),
    record_id: text(item.record_id),
    event_kind: enumValue(item.event_kind, [
      'record_action_assigned', 'record_action_completed', 'record_action_cancelled',
      'record_comment_replied', 'record_comment_mentioned',
    ]),
    subject_kind: enumValue(item.subject_kind, ['action', 'comment']),
    subject_id: text(item.subject_id),
    source_version: integer(item.source_version),
    reason: enumValue(item.reason, ['owner', 'participant', 'assignee', 'mention', 'reply', 'follower', 'security']),
    mandatory: booleanValue(item.mandatory),
    event_at: text(item.event_at),
    read_at: nullableText(item.read_at),
    dismissed_at: nullableText(item.dismissed_at),
  }
}

export function decodeRecordNotificationList(value: unknown): RecordNotificationListResponse {
  const response = objectValue(value)
  if (!Array.isArray(response.items)) invalid()
  return { items: response.items.map(decodeRecordNotification) }
}

export function decodeRecordNotificationTarget(value: unknown): RecordNotificationTarget {
  const item = objectValue(value)
  return {
    record_id: text(item.record_id),
    subject_kind: enumValue(item.subject_kind, ['action', 'comment']),
    subject_id: text(item.subject_id),
  }
}
