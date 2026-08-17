import { requestJSON } from './apiRequest'
import type { RecordNotificationUnreadResponse } from './types'

export function getRecordNotificationUnreadCount(): Promise<RecordNotificationUnreadResponse> {
  return requestJSON<unknown>('/api/record-notifications/unread-count').then((value) => {
    if (typeof value !== 'object' || value === null || Array.isArray(value)) {
      throw new Error('invalid_record_notification_unread_response')
    }
    const unreadCount = (value as Record<string, unknown>).unread_count
    if (!Number.isSafeInteger(unreadCount) || (unreadCount as number) < 0) {
      throw new Error('invalid_record_notification_unread_response')
    }
    return { unread_count: unreadCount as number }
  })
}
