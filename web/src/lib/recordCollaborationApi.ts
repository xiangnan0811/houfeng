import allowlistedApiError from './apiError'
import { jsonBodyInit, requestJSON as transportRequestJSON, withQuery } from './apiRequest'
import type {
  RecordActionInput,
  RecordActionListResponse,
  RecordActionMutation,
  RecordActionTransition,
  RecordCommentInput,
  RecordCommentListResponse,
  RecordCommentMutation,
  RecordFollowerPreference,
  RecordNotification,
  RecordNotificationListResponse,
  RecordNotificationTarget,
  RecordWatch,
} from './types'
import {
  decodeRecordActionList,
  decodeRecordActionMutation,
  decodeRecordCommentList,
  decodeRecordCommentMutation,
  decodeRecordNotification,
  decodeRecordNotificationList,
  decodeRecordNotificationTarget,
  decodeRecordWatch,
} from './recordCollaborationDto'

function encoded(value: string): string {
  return encodeURIComponent(value)
}

function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  return transportRequestJSON<T>(path, init, allowlistedApiError)
}

function commandHeaders(idempotencyKey: string, version?: number): Record<string, string> {
  const headers: Record<string, string> = { 'Idempotency-Key': idempotencyKey }
  if (version !== undefined) headers['If-Match'] = `"${version}"`
  return headers
}

export function listRecordActions(recordId: string, limit = 50): Promise<RecordActionListResponse> {
  return requestJSON<unknown>(withQuery(
    `/api/records/${encoded(recordId)}/actions`, { limit },
  )).then(decodeRecordActionList)
}

export function createRecordAction(
  recordId: string,
  input: RecordActionInput,
  idempotencyKey: string,
): Promise<RecordActionMutation> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/actions`,
    jsonBodyInit('POST', input, commandHeaders(idempotencyKey)),
  ).then(decodeRecordActionMutation)
}

export function updateRecordAction(
  recordId: string,
  actionId: string,
  input: RecordActionInput,
  version: number,
  idempotencyKey: string,
): Promise<RecordActionMutation> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/actions/${encoded(actionId)}`,
    jsonBodyInit('PATCH', input, commandHeaders(idempotencyKey, version)),
  ).then(decodeRecordActionMutation)
}

export function transitionRecordAction(
  recordId: string,
  actionId: string,
  transition: RecordActionTransition,
  version: number,
  idempotencyKey: string,
): Promise<RecordActionMutation> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/actions/${encoded(actionId)}/${transition}`,
    jsonBodyInit('POST', {}, commandHeaders(idempotencyKey, version)),
  ).then(decodeRecordActionMutation)
}

export function listRecordComments(recordId: string, limit = 100): Promise<RecordCommentListResponse> {
  return requestJSON<unknown>(withQuery(
    `/api/records/${encoded(recordId)}/comments`, { limit },
  )).then(decodeRecordCommentList)
}

export function createRecordComment(
  recordId: string,
  input: RecordCommentInput,
  idempotencyKey: string,
): Promise<RecordCommentMutation> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/comments`,
    jsonBodyInit('POST', input, commandHeaders(idempotencyKey)),
  ).then(decodeRecordCommentMutation)
}

export function editRecordComment(
  recordId: string,
  commentId: string,
  input: Omit<RecordCommentInput, 'reply_to_comment_id'>,
  version: number,
  idempotencyKey: string,
): Promise<RecordCommentMutation> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/comments/${encoded(commentId)}`,
    jsonBodyInit('PATCH', input, commandHeaders(idempotencyKey, version)),
  ).then(decodeRecordCommentMutation)
}

export function redactRecordComment(
  recordId: string,
  commentId: string,
  version: number,
  idempotencyKey: string,
): Promise<RecordCommentMutation> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/comments/${encoded(commentId)}/redact`,
    jsonBodyInit('POST', {}, commandHeaders(idempotencyKey, version)),
  ).then(decodeRecordCommentMutation)
}

export function getRecordWatch(recordId: string): Promise<RecordWatch> {
  return requestJSON<unknown>(`/api/records/${encoded(recordId)}/watch`).then(decodeRecordWatch)
}

export function setRecordWatch(
  recordId: string,
  preference: RecordFollowerPreference,
  version: number,
  idempotencyKey: string,
): Promise<RecordWatch> {
  return requestJSON<unknown>(
    `/api/records/${encoded(recordId)}/watch`,
    jsonBodyInit('PATCH', { preference }, commandHeaders(idempotencyKey, version)),
  ).then(decodeRecordWatch)
}

export function listRecordNotifications(limit = 50): Promise<RecordNotificationListResponse> {
  return requestJSON<unknown>(withQuery('/api/record-notifications', { limit })).then(decodeRecordNotificationList)
}

export function getRecordNotification(notificationId: string): Promise<RecordNotification> {
  return requestJSON<unknown>(`/api/record-notifications/${encoded(notificationId)}`).then(decodeRecordNotification)
}

export function getRecordNotificationTarget(notificationId: string): Promise<RecordNotificationTarget> {
  return requestJSON<unknown>(`/api/record-notifications/${encoded(notificationId)}/target`).then(decodeRecordNotificationTarget)
}

function transitionRecordNotification(notificationId: string, transition: 'read' | 'unread' | 'dismiss'): Promise<RecordNotification> {
  return requestJSON<unknown>(`/api/record-notifications/${encoded(notificationId)}/${transition}`, {
    method: 'PUT',
  }).then(decodeRecordNotification)
}

export function markRecordNotificationRead(notificationId: string): Promise<RecordNotification> {
  return transitionRecordNotification(notificationId, 'read')
}

export function markRecordNotificationUnread(notificationId: string): Promise<RecordNotification> {
  return transitionRecordNotification(notificationId, 'unread')
}

export function dismissRecordNotification(notificationId: string): Promise<RecordNotification> {
  return transitionRecordNotification(notificationId, 'dismiss')
}
