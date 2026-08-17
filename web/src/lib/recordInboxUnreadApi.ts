import { requestJSON } from './apiRequest'
import type { RecordNotificationUnreadResponse } from './types'

export const RECORD_INBOX_UNREAD_INVALIDATED_EVENT = 'houfeng:record-inbox-unread-invalidated'

export function getRecordNotificationUnreadCount(): Promise<RecordNotificationUnreadResponse> {
  return requestJSON<unknown>('/api/record-notifications/unread-count').then((value) => {
    if (typeof value !== 'object' || value === null || Array.isArray(value)) {
      throw new Error('invalid_record_notification_unread_response')
    }
    const response = value as Record<string, unknown>
    if (Object.keys(response).length !== 1 || !Object.hasOwn(response, 'unread_count')) {
      throw new Error('invalid_record_notification_unread_response')
    }
    const unreadCount = response.unread_count
    if (!Number.isSafeInteger(unreadCount) || (unreadCount as number) < 0) {
      throw new Error('invalid_record_notification_unread_response')
    }
    return { unread_count: unreadCount as number }
  })
}

export function invalidateRecordNotificationUnreadCount(): void {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(RECORD_INBOX_UNREAD_INVALIDATED_EVENT))
}
