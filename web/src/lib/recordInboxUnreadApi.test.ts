import { afterEach, describe, expect, it, vi } from 'vitest'

import { getRecordNotificationUnreadCount } from './recordInboxUnreadApi'

afterEach(() => vi.restoreAllMocks())

describe('record inbox eager unread transport', () => {
  it('owns only the bounded unread-count request', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ unread_count: 7 }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))

    await expect(getRecordNotificationUnreadCount()).resolves.toEqual({ unread_count: 7 })
    expect(fetchMock).toHaveBeenCalledWith('/api/record-notifications/unread-count', {
      headers: { Accept: 'application/json' }, cache: 'no-store', credentials: 'include',
    })
  })

  it('fails closed for malformed or over-broad unread count responses', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ unread_count: 3, private_title: 'secret' }), { status: 200 }))

    await expect(getRecordNotificationUnreadCount()).rejects.toThrow('invalid_record_notification_unread_response')
  })
})
