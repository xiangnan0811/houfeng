import { useState, type FormEvent } from 'react'

import type { RecordComment } from '../lib/types'
import { formatDateTime } from '../lib/format'
import { RecordCommentMarkdown } from './RecordCommentMarkdown'
import type { RecordCollaborationMemberOption } from './RecordRevisionCollaborationControls'
import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'
import { Button, Modal } from './atoms'

export type RecordCommentSubmit = {
  mode: 'create' | 'edit'
  comment_id: string
  version: number
  body_markdown: string
  reply_to_comment_id: string
  mention_user_ids: string[]
}

type RecordCommentThreadProps = {
  state: RecordCollaborationSurfaceState
  comments: readonly RecordComment[]
  currentUserId: string
  members: readonly RecordCollaborationMemberOption[]
  busy: boolean
  onSubmit: (input: RecordCommentSubmit) => void
  onRedact: (comment: RecordComment) => void
}

export function RecordCommentThread({ state, comments, currentUserId, members, busy, onSubmit, onRedact }: RecordCommentThreadProps) {
  if (state === 'loading' || state === 'error' || state === 'revoked' || state === 'deleted') {
    return <RecordCollaborationState state={state} loadingTitle="正在读取评论" emptyTitle="暂无评论" errorTitle="评论暂不可用" />
  }
  const freshStateKey = `${state}:${comments.map((comment) => `${comment.comment_id}:${comment.state}`).join(',')}`
  return <ReadyRecordCommentThread key={freshStateKey} state={state} comments={comments} currentUserId={currentUserId}
    members={members} busy={busy} onSubmit={onSubmit} onRedact={onRedact} />
}

function ReadyRecordCommentThread({ state, comments, currentUserId, members, busy, onSubmit, onRedact }: RecordCommentThreadProps) {
  const [body, setBody] = useState('')
  const [replyTo, setReplyTo] = useState('')
  const [editingId, setEditingId] = useState('')
  const [mentions, setMentions] = useState<string[]>([])
  const [redactCandidateId, setRedactCandidateId] = useState('')
  const editing = comments.find((comment) => comment.comment_id === editingId && comment.state === 'active') ?? null
  const redactCandidate = comments.find((comment) => comment.comment_id === redactCandidateId && comment.state === 'active') ?? null

  function submit(event: FormEvent) {
    event.preventDefault()
    if (!body.trim() || busy) return
    onSubmit({
      mode: editing ? 'edit' : 'create', comment_id: editing?.comment_id ?? '', version: editing?.version ?? 0,
      body_markdown: body, reply_to_comment_id: editing ? '' : replyTo,
      mention_user_ids: [...mentions].sort(),
    })
  }

  function beginReply(comment: RecordComment) {
    setEditingId('')
    setReplyTo(comment.comment_id)
    setBody('')
    setMentions([])
  }

  function beginEdit(comment: RecordComment) {
    setEditingId(comment.comment_id)
    setReplyTo('')
    setBody('')
    setMentions([])
  }

  function cancelComposer() {
    setEditingId('')
    setReplyTo('')
    setBody('')
    setMentions([])
  }

  function toggleMention(userId: string, selected: boolean) {
    setMentions((current) => selected
      ? [...new Set([...current, userId])].sort()
      : current.filter((value) => value !== userId))
  }

  return (
    <section className="record-collaboration-panel record-comment-thread card" aria-labelledby="record-comments-title">
      <header className="record-collaboration-panel__header section-heading">
        <div><p className="record-collaboration-panel__eyebrow section-heading__eyebrow">COMMENT LOG</p><h2 className="section-heading__title" id="record-comments-title">协作评论</h2></div>
        <span className="record-collaboration-panel__count badge badge--count">{comments.length}</span>
      </header>
      {state === 'empty' || comments.length === 0
        ? <RecordCollaborationState state="empty" loadingTitle="正在读取评论" emptyTitle="暂无评论" errorTitle="评论暂不可用" />
        : (
          <ol className="record-comment-list vps-create-form">
            {comments.map((comment) => (
              <li key={comment.comment_id} className={`record-comment record-comment--${comment.state} card card--dim`}>
                <header className="record-comment__header">
                  <strong>{comment.author_id}</strong>
                  <span>{formatDateTime(comment.updated_at)} · v{comment.version}</span>
                </header>
                {comment.state === 'redacted' ? (
                  <p className="record-comment__redacted">评论内容已永久遮盖</p>
                ) : comment.render_model !== null && comment.body_markdown !== null ? (
                  <RecordCommentMarkdown model={comment.render_model} />
                ) : <p className="record-comment__redacted">评论内容暂不可用</p>}
                {comment.state === 'active' ? (
                  <div className="record-comment__commands page-form-actions">
                    <Button size="sm" variant="ghost" disabled={busy} onClick={() => beginReply(comment)}>回复该评论</Button>
                    {comment.author_id === currentUserId
                      ? <Button size="sm" variant="ghost" disabled={busy} onClick={() => beginEdit(comment)}>编辑该评论</Button>
                      : null}
                    <Button size="sm" variant="secondary" disabled={busy} onClick={() => setRedactCandidateId(comment.comment_id)}>请求遮盖该评论</Button>
                  </div>
                ) : null}
              </li>
            ))}
          </ol>
        )}
      <form className="record-collaboration-composer record-comment-composer vps-create-form" onSubmit={submit}>
        <div className="record-comment-composer__context">
          {editing ? `正在编辑 ${editing.comment_id}` : replyTo ? `正在回复 ${replyTo}` : '发布新评论'}
        </div>
        <label className="input-field__label" htmlFor="record-comment-body">评论内容</label>
        <textarea id="record-comment-body" className="input record-comment-composer__body" value={body}
          minLength={1} maxLength={16_384} required disabled={busy} onChange={(event) => setBody(event.target.value)} />
        <fieldset className="record-collaboration-members" disabled={busy}>
          <legend>提及成员</legend>
          <div className="record-collaboration-members__list asset-option-grid">
            {members.map((member) => (
              <label key={member.id} className="record-collaboration-member asset-option-radio">
                <input type="checkbox" checked={mentions.includes(member.id)}
                  onChange={(event) => toggleMention(member.id, event.target.checked)} />
                <span>提及{member.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="record-collaboration-composer__commands page-form-actions">
          <Button type="submit" disabled={busy || !body.trim()}>{editing ? '保存编辑' : replyTo ? '发布回复' : '发布评论'}</Button>
          {editing || replyTo ? <Button variant="ghost" disabled={busy} onClick={cancelComposer}>取消</Button> : null}
        </div>
      </form>
      <Modal open={redactCandidate !== null} onClose={() => setRedactCandidateId('')}
        title="确认永久遮盖评论" dialogRole="alertdialog" size="sm" persistent={busy}
        footer={<>
          <Button variant="ghost" disabled={busy} onClick={() => setRedactCandidateId('')}>取消遮盖</Button>
          <Button variant="secondary" disabled={busy} onClick={() => {
            if (redactCandidate === null) return
            onRedact(redactCandidate)
            setRedactCandidateId('')
          }}>确认永久遮盖</Button>
        </>}
      >
        <p className="inline-alert warn">遮盖不可撤销；正文、历史渲染与摘要都将被清除。</p>
      </Modal>
    </section>
  )
}
