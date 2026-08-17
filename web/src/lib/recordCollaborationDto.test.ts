import { describe, expect, it } from 'vitest'

import {
  decodeRecordAction,
  decodeRecordComment,
  decodeRecordCommentList,
  decodeRecordNotification,
  decodeRecordNotificationTarget,
  decodeRecordWatch,
} from './recordCollaborationDto'

const userID = 'usr_0123456789abcdef01234567'
const notificationID = `rnt_${'a'.repeat(64)}`
const timestamp = '2026-08-17T06:00:00Z'

const action = {
  action_id: 'ract_action1', record_id: 'rec_record1', version: 1, status: 'open', title: '排查告警',
	 details: '先核对源端，再执行恢复。',
  assignee_id: userID, due_at: null, completed_at: null, subject_revision_id: 'rrv_revision1',
  created_at: timestamp, updated_at: timestamp,
}

const comment = {
  comment_id: 'rcm_comment1', record_id: 'rec_record1', author_id: userID, version: 2, state: 'active',
  body_markdown: '已处理',
  render_model: { version: 'comment_markdown/v1', nodes: [{ type: 'paragraph', children: [{ type: 'text', text: '已处理' }] }] },
  reply_to_comment_id: '', mention_user_ids: [userID], created_at: timestamp, updated_at: timestamp, redacted_at: null,
}

const notification = {
  notification_id: notificationID, record_id: 'rec_record1', event_kind: 'comment_mentioned',
  subject_kind: 'comment', subject_id: 'rcm_comment1', source_version: 2, reason: 'mention', mandatory: true,
  event_at: timestamp, read_at: null, dismissed_at: null,
}

describe('record collaboration DTO decoders', () => {
  it.each([
    ['action', () => decodeRecordAction({ ...action, private_note: 'must not cross' })],
    ['comment', () => decodeRecordComment({ ...comment, private_note: 'must not cross' })],
    ['notification', () => decodeRecordNotification({ ...notification, private_note: 'must not cross' })],
    ['target', () => decodeRecordNotificationTarget({ record_id: 'rec_record1', subject_kind: 'comment', subject_id: 'rcm_comment1', private_note: 'must not cross' })],
    ['watch', () => decodeRecordWatch({
      record_id: 'rec_record1', user_id: userID, version: 0, preference: 'default',
      sources: { author: false, owner: false, participant: false, comment: false, mention: false, action: false },
      updated_at: null, private_note: 'must not cross',
    })],
  ])('rejects unknown %s response keys', (_name, decode) => {
    expect(decode).toThrow('invalid_record_collaboration_response')
  })

  it('rejects non-canonical identities, zero action versions, and non-RFC3339 times', () => {
    expect(() => decodeRecordAction({ ...action, action_id: 'action1' })).toThrow()
    expect(() => decodeRecordAction({ ...action, version: 0 })).toThrow()
    expect(() => decodeRecordAction({ ...action, created_at: '17 August 2026' })).toThrow()
  })

  it('enforces action completion and timestamp relationships', () => {
    expect(() => decodeRecordAction({ ...action, status: 'completed' })).toThrow()
    expect(() => decodeRecordAction({ ...action, updated_at: '2026-08-17T05:59:59Z' })).toThrow()
  })

  it('uses the closed comment render decoder and returns a detached model', () => {
    const source = structuredClone(comment)
    const decoded = decodeRecordComment(source)
    source.render_model.nodes[0]!.children[0]!.text = 'changed'
    expect(decoded.render_model).toEqual(comment.render_model)
    expect(() => decodeRecordComment({
      ...comment,
      render_model: { ...comment.render_model, raw_html: '<img src=x>' },
    })).toThrow()
  })

  it('enforces redacted comment absence and sorted unique mentions', () => {
    expect(() => decodeRecordComment({ ...comment, mention_user_ids: [userID, userID] })).toThrow()
    expect(() => decodeRecordComment({ ...comment, state: 'redacted', redacted_at: timestamp })).toThrow()
  })

	it.each([100, 101, 200])('accepts the backend comment list boundary %i', (count) => {
		expect(decodeRecordCommentList({ comments: Array.from({ length: count }, () => comment) }).comments).toHaveLength(count)
	})

	it('rejects comment lists above the backend limit', () => {
		expect(() => decodeRecordCommentList({ comments: Array.from({ length: 201 }, () => comment) })).toThrow()
	})

  it('enforces notification event, subject, reason, mandatory, and timeline relationships', () => {
    expect(() => decodeRecordNotification({ ...notification, subject_kind: 'action', subject_id: 'ract_action1' })).toThrow()
    expect(() => decodeRecordNotification({ ...notification, mandatory: false })).toThrow()
    expect(() => decodeRecordNotification({ ...notification, read_at: '2026-08-17T05:59:59Z' })).toThrow()
    expect(() => decodeRecordNotification({ ...notification, dismissed_at: timestamp })).toThrow()
  })

  it('accepts the exact version-zero default watch empty state only', () => {
    const emptyWatch = {
      record_id: 'rec_record1', user_id: userID, version: 0, preference: 'default',
      sources: { author: false, owner: false, participant: false, comment: false, mention: false, action: false },
      updated_at: null,
    }
    expect(decodeRecordWatch(emptyWatch)).toEqual(emptyWatch)
    expect(() => decodeRecordWatch({ ...emptyWatch, preference: 'watching' })).toThrow()
  })

	it('accepts a positive-version default watch replay anchor without automatic sources', () => {
		const anchor = {
			record_id: 'rec_record1', user_id: userID, version: 1, preference: 'default',
			sources: { author: false, owner: false, participant: false, comment: false, mention: false, action: false },
			updated_at: timestamp,
		}
		expect(decodeRecordWatch(anchor)).toEqual(anchor)
		expect(() => decodeRecordWatch({ ...anchor, updated_at: null })).toThrow()
	})
})
