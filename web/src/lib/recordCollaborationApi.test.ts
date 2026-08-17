import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createRecordAction,
  createRecordComment,
  dismissRecordNotification,
  getRecordNotificationTarget,
  getRecordWatch,
  listRecordActions,
  listRecordComments,
  listRecordNotifications,
  setRecordWatch,
  transitionRecordAction,
} from './recordCollaborationApi'

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const actionMutation = {
  action_id: 'ract_one', record_id: 'rec_one', version: 1, status: 'open', event_kind: 'created',
  replayed: false, changed_at: '2026-08-17T09:00:00Z',
}
const commentMutation = {
  comment_id: 'rcm_one', record_id: 'rec_one', version: 1, state: 'active', event_kind: 'created',
  replayed: false, changed_at: '2026-08-17T09:00:00Z',
}
const watch = {
  record_id: 'rec_one', user_id: 'usr_one', version: 0, preference: 'default',
  sources: { author: false, owner: true, participant: false, comment: false, mention: false, action: false },
  updated_at: null,
}
const notification = {
  notification_id: 'rnt_one', record_id: 'rec_one', event_kind: 'record_comment_mentioned',
  subject_kind: 'comment', subject_id: 'rcm_one', source_version: 1, reason: 'mention', mandatory: true,
  event_at: '2026-08-17T09:00:00Z', read_at: null, dismissed_at: null,
}

afterEach(() => vi.restoreAllMocks())

describe('record collaboration lazy transport', () => {
  it('lists the bounded action/comment/inbox read models', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response({ items: [] }))
      .mockResolvedValueOnce(response({ comments: [] }))
      .mockResolvedValueOnce(response({ items: [] }))

    await expect(listRecordActions('rec /one', 25)).resolves.toEqual({ items: [] })
    await expect(listRecordComments('rec /one', 40)).resolves.toEqual({ comments: [] })
    await expect(listRecordNotifications(50)).resolves.toEqual({ items: [] })

    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      '/api/records/rec%20%2Fone/actions?limit=25',
      '/api/records/rec%20%2Fone/comments?limit=40',
      '/api/record-notifications?limit=50',
    ])
  })

  it('sends exact action/comment/watch command headers and allowlisted bodies', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response(actionMutation))
      .mockResolvedValueOnce(response({ ...actionMutation, version: 2, status: 'completed', event_kind: 'completed' }))
      .mockResolvedValueOnce(response(commentMutation))
      .mockResolvedValueOnce(response(watch))

    await createRecordAction('rec_one', {
      title: '复核', details: '', assignee_id: '', due_at: null, subject_revision_id: '',
    }, 'action-key')
    await transitionRecordAction('rec_one', 'ract_one', 'complete', 3, 'complete-key')
    await createRecordComment('rec_one', {
      body_markdown: '安全内容', reply_to_comment_id: '', mention_user_ids: [],
    }, 'comment-key')
    await setRecordWatch('rec_one', 'watching', 0, 'watch-key')

    expect(fetchMock.mock.calls.map(([path, init]) => [path, init?.method, init?.headers, init?.body])).toEqual([
      ['/api/records/rec_one/actions', 'POST', expect.objectContaining({ 'Idempotency-Key': 'action-key' }), JSON.stringify({
        title: '复核', details: '', assignee_id: '', due_at: null, subject_revision_id: '',
      })],
      ['/api/records/rec_one/actions/ract_one/complete', 'POST', expect.objectContaining({
        'Idempotency-Key': 'complete-key', 'If-Match': '"3"',
      }), JSON.stringify({})],
      ['/api/records/rec_one/comments', 'POST', expect.objectContaining({ 'Idempotency-Key': 'comment-key' }), JSON.stringify({
        body_markdown: '安全内容', reply_to_comment_id: '', mention_user_ids: [],
      })],
      ['/api/records/rec_one/watch', 'PATCH', expect.objectContaining({
        'Idempotency-Key': 'watch-key', 'If-Match': '"0"',
      }), JSON.stringify({ preference: 'watching' })],
    ])
  })

  it('uses empty transition bodies and keeps deep-link targets typed', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response({ record_id: 'rec_one', subject_kind: 'comment', subject_id: 'rcm_one' }))
      .mockResolvedValueOnce(response(notification))
      .mockResolvedValueOnce(response(watch))

    await expect(getRecordNotificationTarget('rnt_one')).resolves.toEqual({
      record_id: 'rec_one', subject_kind: 'comment', subject_id: 'rcm_one',
    })
    await dismissRecordNotification('rnt_one')
    await getRecordWatch('rec_one')

    const dismissCall = fetchMock.mock.calls.find(([path]) => path === '/api/record-notifications/rnt_one/dismiss')
    expect(dismissCall?.[1]).toMatchObject({ method: 'PUT' })
    expect(dismissCall?.[1]).not.toHaveProperty('body')
  })

  it('decodes a closed notification DTO and rejects nil collection fallbacks', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response({ items: [{ ...notification, private_record_title: 'do not render' }] }))
      .mockResolvedValueOnce(response({ items: null }))

    await expect(listRecordNotifications()).resolves.toEqual({ items: [notification] })
    await expect(listRecordNotifications()).rejects.toThrow('invalid_record_collaboration_response')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
