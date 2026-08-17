import { decodeCommentRenderModelV1 } from './commentMarkdown'
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
const maxCommentSourceBytes = 16_384

function invalid(): never {
  throw new Error(invalidResponse)
}

function objectValue(value: unknown, expectedKeys: readonly string[]): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) invalid()
  const object = value as Record<string, unknown>
  const actual = Object.keys(object).sort()
  const expected = [...expectedKeys].sort()
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) invalid()
  return object
}

function text(value: unknown, allowEmpty = true): string {
  if (typeof value !== 'string' || (!allowEmpty && value.length === 0) || !isWellFormedUnicode(value)) invalid()
  return value
}

function nullableText(value: unknown): string | null {
  if (value === null) return null
  return text(value)
}

function integer(value: unknown, allowZero = false): number {
  if (!Number.isSafeInteger(value) || (allowZero ? (value as number) < 0 : (value as number) < 1)) invalid()
  return value as number
}

function booleanValue(value: unknown): boolean {
  if (typeof value !== 'boolean') invalid()
  return value
}

function enumValue<const T extends string>(value: unknown, values: readonly T[]): T {
  if (typeof value !== 'string' || !values.includes(value as T)) invalid()
  return value as T
}

function prefixedID(value: unknown, prefix: string, allowEmpty = false): string {
  const candidate = text(value, allowEmpty)
  if (allowEmpty && candidate === '') return candidate
  const suffix = candidate.slice(prefix.length)
  if (!candidate.startsWith(prefix) || suffix.length < 1 || suffix.length > 64 || !/^[a-z0-9]+$/u.test(suffix)) invalid()
  return candidate
}

function userID(value: unknown, allowEmpty = false): string {
  const candidate = text(value, allowEmpty)
  if (allowEmpty && candidate === '') return candidate
  if (!/^usr_[a-f0-9]{24}$/u.test(candidate)) invalid()
  return candidate
}

function notificationID(value: unknown): string {
  const candidate = text(value, false)
  if (!/^rnt_[a-f0-9]{64}$/u.test(candidate)) invalid()
  return candidate
}

function timestamp(value: unknown): string {
  const candidate = text(value, false)
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?Z$/u.exec(candidate)
  if (match === null || (match[7]?.endsWith('0') ?? false)) invalid()
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3])
  const hour = Number(match[4]); const minute = Number(match[5]); const second = Number(match[6])
  const millis = Date.UTC(year, month - 1, day, hour, minute, second)
  const parsed = new Date(millis)
  if (!Number.isFinite(millis) || parsed.getUTCFullYear() !== year || parsed.getUTCMonth() + 1 !== month ||
    parsed.getUTCDate() !== day || parsed.getUTCHours() !== hour || parsed.getUTCMinutes() !== minute || parsed.getUTCSeconds() !== second) invalid()
  return candidate
}

function nullableTimestamp(value: unknown): string | null {
  if (value === null) return null
  return timestamp(value)
}

function timestampOrder(value: string): bigint {
  const [whole = '', fractionWithZone = ''] = value.split('.')
  const secondPart = fractionWithZone === '' ? whole.slice(0, -1) : whole
  const seconds = BigInt(Math.floor(Date.parse(`${secondPart}Z`) / 1_000))
  const fraction = fractionWithZone === '' ? '' : fractionWithZone.slice(0, -1)
  return seconds * 1_000_000_000n + BigInt(fraction.padEnd(9, '0') || '0')
}

function sortedUniqueUserIDs(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > 512) invalid()
  const result = value.map((item) => userID(item))
  if (result.some((item, index) => index > 0 && item <= result[index - 1]!)) invalid()
  return result
}

function decodeItems<T>(value: unknown, maximum: number, decode: (item: unknown) => T): T[] {
  if (!Array.isArray(value) || value.length > maximum) invalid()
  return value.map(decode)
}

export function decodeRecordAction(value: unknown): RecordAction {
  const item = objectValue(value, [
		'action_id', 'assignee_id', 'completed_at', 'created_at', 'details', 'due_at', 'record_id',
    'status', 'subject_revision_id', 'title', 'updated_at', 'version',
  ])
  const status = enumValue(item.status, ['open', 'completed', 'cancelled'])
  const createdAt = timestamp(item.created_at)
  const updatedAt = timestamp(item.updated_at)
  const completedAt = nullableTimestamp(item.completed_at)
  const title = text(item.title, false)
	const details = text(item.details)
  if (timestampOrder(updatedAt) < timestampOrder(createdAt) ||
    (status === 'completed') !== (completedAt !== null) ||
    (completedAt !== null && timestampOrder(completedAt) < timestampOrder(createdAt)) ||
		title.trim() !== title || Array.from(title).length > 512 || hasControlCharacter(title) ||
		Array.from(details).length > 4_096 || hasDisallowedMultilineControl(details)) invalid()
  return {
    action_id: prefixedID(item.action_id, 'ract_'),
    record_id: prefixedID(item.record_id, 'rec_'),
    version: integer(item.version),
    status,
    title,
		details,
    assignee_id: userID(item.assignee_id, true),
    due_at: nullableTimestamp(item.due_at),
    completed_at: completedAt,
    subject_revision_id: prefixedID(item.subject_revision_id, 'rrv_', true),
    created_at: createdAt,
    updated_at: updatedAt,
  }
}

export function decodeRecordActionList(value: unknown): RecordActionListResponse {
  const response = objectValue(value, ['items'])
  return { items: decodeItems(response.items, 100, decodeRecordAction) }
}

export function decodeRecordActionMutation(value: unknown): RecordActionMutation {
  const item = objectValue(value, ['action_id', 'changed_at', 'event_kind', 'record_id', 'replayed', 'status', 'version'])
  const status = enumValue(item.status, ['open', 'completed', 'cancelled'])
  const eventKind = enumValue(item.event_kind, ['created', 'updated', 'completed', 'cancelled', 'reopened'])
  if ((eventKind === 'created' || eventKind === 'reopened') && status !== 'open') invalid()
  if (eventKind === 'completed' && status !== 'completed') invalid()
  if (eventKind === 'cancelled' && status !== 'cancelled') invalid()
  return {
    action_id: prefixedID(item.action_id, 'ract_'), record_id: prefixedID(item.record_id, 'rec_'),
    version: integer(item.version), status, event_kind: eventKind,
    replayed: booleanValue(item.replayed), changed_at: timestamp(item.changed_at),
  }
}

export function decodeRecordComment(value: unknown): RecordComment {
  const item = objectValue(value, [
    'author_id', 'body_markdown', 'comment_id', 'created_at', 'mention_user_ids', 'record_id',
    'redacted_at', 'render_model', 'reply_to_comment_id', 'state', 'updated_at', 'version',
  ])
  const state = enumValue(item.state, ['active', 'redacted'])
  const body = nullableText(item.body_markdown)
  const redactedAt = nullableTimestamp(item.redacted_at)
  const createdAt = timestamp(item.created_at)
  const updatedAt = timestamp(item.updated_at)
  const mentions = sortedUniqueUserIDs(item.mention_user_ids)
  let renderModel = null
  if (state === 'active') {
    if (body === null || body.length === 0 || new TextEncoder().encode(body).length > maxCommentSourceBytes || item.render_model === null || redactedAt !== null) invalid()
    renderModel = decodeCommentRenderModelV1(item.render_model)
  } else if (body !== null || item.render_model !== null || redactedAt === null || mentions.length !== 0) {
    invalid()
  }
  if (timestampOrder(updatedAt) < timestampOrder(createdAt) ||
    (redactedAt !== null && timestampOrder(redactedAt) < timestampOrder(createdAt))) invalid()
  return {
    comment_id: prefixedID(item.comment_id, 'rcm_'), record_id: prefixedID(item.record_id, 'rec_'),
    author_id: userID(item.author_id), version: integer(item.version), state,
    body_markdown: body, render_model: renderModel,
    reply_to_comment_id: prefixedID(item.reply_to_comment_id, 'rcm_', true), mention_user_ids: mentions,
    created_at: createdAt, updated_at: updatedAt, redacted_at: redactedAt,
  }
}

export function decodeRecordCommentList(value: unknown): RecordCommentListResponse {
  const response = objectValue(value, ['comments'])
	return { comments: decodeItems(response.comments, 200, decodeRecordComment) }
}

export function decodeRecordCommentMutation(value: unknown): RecordCommentMutation {
  const item = objectValue(value, ['changed_at', 'comment_id', 'event_kind', 'record_id', 'replayed', 'state', 'version'])
  const state = enumValue(item.state, ['active', 'redacted'])
  const eventKind = enumValue(item.event_kind, ['created', 'edited', 'redacted'])
  if ((eventKind === 'redacted') !== (state === 'redacted')) invalid()
  return {
    comment_id: prefixedID(item.comment_id, 'rcm_'), record_id: prefixedID(item.record_id, 'rec_'),
    version: integer(item.version), state, event_kind: eventKind,
    replayed: booleanValue(item.replayed), changed_at: timestamp(item.changed_at),
  }
}

export function decodeRecordWatch(value: unknown): RecordWatch {
  const item = objectValue(value, ['preference', 'record_id', 'sources', 'updated_at', 'user_id', 'version'])
  const sources = objectValue(item.sources, ['action', 'author', 'comment', 'mention', 'owner', 'participant'])
  const decodedSources = {
    author: booleanValue(sources.author), owner: booleanValue(sources.owner),
    participant: booleanValue(sources.participant), comment: booleanValue(sources.comment),
    mention: booleanValue(sources.mention), action: booleanValue(sources.action),
  }
  const version = integer(item.version, true)
  const preference = enumValue(item.preference, ['default', 'watching', 'muted'])
  const updatedAt = nullableTimestamp(item.updated_at)
  const hasSources = Object.values(decodedSources).some(Boolean)
  if (version === 0) {
    if (preference !== 'default' || hasSources || updatedAt !== null) invalid()
  } else if (updatedAt === null || (preference === 'default' && !hasSources)) invalid()
  return {
    record_id: prefixedID(item.record_id, 'rec_'), user_id: userID(item.user_id), version, preference,
    sources: decodedSources, updated_at: updatedAt,
  }
}

const notificationEvents = [
  'record_owner_changed', 'record_participant_changed', 'record_follow_up_due',
  'action_assigned', 'action_completed', 'action_cancelled',
  'comment_replied', 'comment_mentioned', 'security_access_revoked',
] as const

function expectedSubjectKind(event: typeof notificationEvents[number]): RecordNotification['subject_kind'] {
  if (event.startsWith('action_')) return 'action'
  if (event.startsWith('comment_')) return 'comment'
  return 'record'
}

export function decodeRecordNotification(value: unknown): RecordNotification {
  const item = objectValue(value, [
    'dismissed_at', 'event_at', 'event_kind', 'mandatory', 'notification_id', 'read_at',
    'reason', 'record_id', 'source_version', 'subject_id', 'subject_kind',
  ])
  const eventKind = enumValue(item.event_kind, notificationEvents)
  const subjectKind = enumValue(item.subject_kind, ['record', 'action', 'comment'])
  const recordID = prefixedID(item.record_id, 'rec_')
  const subjectID = subjectKind === 'record' ? prefixedID(item.subject_id, 'rec_')
    : subjectKind === 'action' ? prefixedID(item.subject_id, 'ract_') : prefixedID(item.subject_id, 'rcm_')
  const reason = enumValue(item.reason, ['owner', 'participant', 'assignee', 'mention', 'reply', 'follower', 'security'])
  const mandatory = booleanValue(item.mandatory)
  const eventAt = timestamp(item.event_at)
  const readAt = nullableTimestamp(item.read_at)
  const dismissedAt = nullableTimestamp(item.dismissed_at)
  if (subjectKind !== expectedSubjectKind(eventKind) || (subjectKind === 'record' && subjectID !== recordID) ||
    mandatory !== ['assignee', 'mention', 'security'].includes(reason) ||
    (readAt !== null && timestampOrder(readAt) < timestampOrder(eventAt)) ||
    (dismissedAt !== null && (readAt === null || timestampOrder(dismissedAt) < timestampOrder(readAt)))) invalid()
  return {
    notification_id: notificationID(item.notification_id), record_id: recordID, event_kind: eventKind,
    subject_kind: subjectKind, subject_id: subjectID, source_version: integer(item.source_version),
    reason, mandatory, event_at: eventAt, read_at: readAt, dismissed_at: dismissedAt,
  }
}

export function decodeRecordNotificationList(value: unknown): RecordNotificationListResponse {
  const response = objectValue(value, ['items'])
  return { items: decodeItems(response.items, 100, decodeRecordNotification) }
}

export function decodeRecordNotificationTarget(value: unknown): RecordNotificationTarget {
  const item = objectValue(value, ['record_id', 'subject_id', 'subject_kind'])
  const subjectKind = enumValue(item.subject_kind, ['record', 'action', 'comment'])
  const recordID = prefixedID(item.record_id, 'rec_')
  const subjectID = subjectKind === 'record' ? prefixedID(item.subject_id, 'rec_')
    : subjectKind === 'action' ? prefixedID(item.subject_id, 'ract_') : prefixedID(item.subject_id, 'rcm_')
  if (subjectKind === 'record' && subjectID !== recordID) invalid()
  return { record_id: recordID, subject_kind: subjectKind, subject_id: subjectID }
}

function isWellFormedUnicode(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index)
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1)
      if (next < 0xdc00 || next > 0xdfff) return false
      index += 1
    } else if (code >= 0xdc00 && code <= 0xdfff) return false
  }
  return true
}

function hasControlCharacter(value: string): boolean {
  return Array.from(value).some((character) => {
    const code = character.codePointAt(0) ?? 0
    return code < 0x20 || code === 0x7f
  })
}

function hasDisallowedMultilineControl(value: string): boolean {
	return Array.from(value).some((character) => {
		const code = character.codePointAt(0) ?? 0
		return (code < 0x20 && character !== '\n' && character !== '\t') || code === 0x7f
	})
}
