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
  const freshStateKey = `${state}:${comments.map((comment) => `${comment.comment_id}:${comment.state}:${comment.version}`).join(',')}`
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

  const memberLabel = (id: string) => members.find((member) => member.id === id)?.label ?? id
  const replyTarget = comments.find((comment) => comment.comment_id === replyTo) ?? null

  return (
    <section className="record-collaboration-panel record-comment-thread" aria-labelledby="record-comments-title">
      <header className="record-collaboration-panel__header">
        <h2 className="record-collaboration-panel__title" id="record-comments-title">
          评论
          <span className={comments.length > 0 ? 'record-count record-count--active' : 'record-count'}>{comments.length}</span>
        </h2>
      </header>
      {state === 'empty' || comments.length === 0
        ? <p className="record-collaboration-panel__empty">暂无评论</p>
        : (
          <ol className="record-comment-list">
            {comments.map((comment) => (
              <li key={comment.comment_id} className={comment.state === 'redacted' ? 'record-comment record-comment--redacted' : 'record-comment'}>
                <header className="record-comment__header">
                  <strong>{memberLabel(comment.author_id)}</strong>
                  <span>{formatDateTime(comment.updated_at)}</span>
                  {comment.reply_to_comment_id ? <span className="record-comment__reply">回复</span> : null}
                </header>
                {comment.state === 'redacted' ? (
                  <p className="record-comment__redacted">评论内容已永久遮盖</p>
                ) : comment.render_model !== null && comment.body_markdown !== null ? (
                  <div className="record-comment__body"><RecordCommentMarkdown model={comment.render_model} /></div>
                ) : <p className="record-comment__redacted">评论内容暂不可用</p>}
                {comment.state === 'active' ? (
                  <div className="record-comment__commands">
                    <Button size="sm" variant="ghost" disabled={busy} aria-label="回复该评论" onClick={() => beginReply(comment)}>回复</Button>
                    {comment.author_id === currentUserId
                      ? <Button size="sm" variant="ghost" disabled={busy} aria-label="编辑该评论" onClick={() => beginEdit(comment)}>编辑</Button>
                      : null}
                    <Button size="sm" variant="ghost" disabled={busy} aria-label="请求遮盖该评论"
                      onClick={() => setRedactCandidateId(comment.comment_id)}>遮盖</Button>
                  </div>
                ) : null}
              </li>
            ))}
          </ol>
        )}
      <form className="record-collaboration-composer record-comment-composer" onSubmit={submit}>
        {editing || replyTarget ? (
          <p className="record-comment-composer__context">
            {editing ? '编辑评论' : `回复 ${memberLabel(replyTarget?.author_id ?? '')}`}
          </p>
        ) : null}
        <label className="visually-hidden" htmlFor="record-comment-body">评论内容</label>
        <textarea id="record-comment-body" className="input record-comment-composer__body" value={body}
          placeholder={editing ? '替换后的评论内容' : '写下评论，支持 Markdown'}
          minLength={1} maxLength={16_384} required disabled={busy} onChange={(event) => setBody(event.target.value)} />
        <div className="record-comment-composer__footer">
          {members.length > 0 ? (
            <fieldset className="record-collaboration-members" disabled={busy}>
              <legend className="visually-hidden">提及成员</legend>
              {members.map((member) => (
                <label key={member.id} className="record-chip">
                  <input type="checkbox" checked={mentions.includes(member.id)}
                    onChange={(event) => toggleMention(member.id, event.target.checked)} />
                  <span aria-hidden="true">@</span><span className="visually-hidden">提及</span>{member.label}
                </label>
              ))}
            </fieldset>
          ) : null}
          <div className="record-collaboration-composer__commands">
            {editing || replyTo ? <Button size="sm" variant="ghost" disabled={busy} onClick={cancelComposer}>取消</Button> : null}
            <Button size="sm" type="submit" disabled={busy || !body.trim()}>{editing ? '保存编辑' : replyTo ? '发布回复' : '发布评论'}</Button>
          </div>
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
