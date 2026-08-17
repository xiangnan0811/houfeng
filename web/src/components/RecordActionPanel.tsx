import { useState, type FormEvent } from 'react'

import type { RecordAction, RecordActionTransition } from '../lib/types'
import { formatDateTime } from '../lib/format'
import type { RecordCollaborationMemberOption } from './RecordRevisionCollaborationControls'
import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'
import { Button, Input, Select } from './atoms'

export type RecordActionCreateValues = {
  title: string
  details: ''
  assignee_id: string
  due_at: string | null
  subject_revision_id: string
}

export type RecordActionUpdateValues = {
  title: string
  assignee_id: string
  due_at: string | null
  subject_revision_id: string
  version: number
}

type RecordActionPanelProps = {
  state: RecordCollaborationSurfaceState
  actions: readonly RecordAction[]
  members: readonly RecordCollaborationMemberOption[]
  busy: boolean
  onCreate: (values: RecordActionCreateValues) => void
  onUpdate: (action: RecordAction, values: RecordActionUpdateValues) => void
  onTransition: (action: RecordAction, transition: RecordActionTransition) => void
}

export function RecordActionPanel({ state, actions, members, busy, onCreate, onUpdate, onTransition }: RecordActionPanelProps) {
  const [title, setTitle] = useState('')
  const [assigneeId, setAssigneeId] = useState('')
  const [dueAt, setDueAt] = useState('')
  const [subjectRevisionId, setSubjectRevisionId] = useState('')
  const [editingActionId, setEditingActionId] = useState('')
  const editingAction = actions.find((action) => action.action_id === editingActionId) ?? null

  if (state === 'loading' || state === 'error' || state === 'revoked' || state === 'deleted') {
    return <RecordCollaborationState state={state} loadingTitle="正在读取行动项" emptyTitle="暂无行动项" errorTitle="行动项暂不可用" />
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    const normalizedTitle = title.trim()
    if (!normalizedTitle || busy) return
    const values = {
      title: normalizedTitle,
      assignee_id: assigneeId,
      due_at: dueAt ? new Date(dueAt).toISOString() : null,
      subject_revision_id: subjectRevisionId,
    }
    if (editingAction) {
      onUpdate(editingAction, { ...values, version: editingAction.version })
      return
    }
    onCreate({
      ...values,
      details: '',
    })
  }

  function beginEdit(action: RecordAction) {
    setEditingActionId(action.action_id)
    setTitle(action.title)
    setAssigneeId(action.assignee_id)
    setDueAt(toDateTimeLocal(action.due_at))
    setSubjectRevisionId(action.subject_revision_id)
  }

  function resetComposer() {
    setEditingActionId('')
    setTitle('')
    setAssigneeId('')
    setDueAt('')
    setSubjectRevisionId('')
  }

  return (
    <section className="record-collaboration-panel record-action-panel card" aria-labelledby="record-actions-title">
      <header className="record-collaboration-panel__header section-heading">
        <div><p className="record-collaboration-panel__eyebrow section-heading__eyebrow">ACTION QUEUE</p><h2 className="section-heading__title" id="record-actions-title">行动队列</h2></div>
        <span className="record-collaboration-panel__count badge badge--count">{actions.length}</span>
      </header>
      {state === 'empty' || actions.length === 0
        ? <RecordCollaborationState state="empty" loadingTitle="正在读取行动项" emptyTitle="暂无行动项" errorTitle="行动项暂不可用" />
        : (
          <ol className="record-action-list vps-create-form">
            {actions.map((action) => (
              <li key={action.action_id} className={`record-action record-action--${action.status} card card--dim metric-card`}>
                <div className="record-action__index" aria-hidden="true">{String(action.version).padStart(2, '0')}</div>
                <div className="record-action__body">
                  <strong>{action.title}</strong>
                  <span>{action.assignee_id || '未指派'} · {action.due_at ? formatDateTime(action.due_at) : '无截止时间'}</span>
                </div>
                <div className="record-action__commands page-form-actions">
                  <Button size="sm" variant="ghost" disabled={busy} aria-label={`编辑“${action.title}”`}
                    onClick={() => beginEdit(action)}>编辑</Button>
                  {action.status === 'open' ? <>
                    <Button size="sm" variant="secondary" disabled={busy} aria-label={`完成“${action.title}”`}
                      onClick={() => onTransition(action, 'complete')}>完成</Button>
                    <Button size="sm" variant="ghost" disabled={busy} aria-label={`取消“${action.title}”`}
                      onClick={() => onTransition(action, 'cancel')}>取消</Button>
                  </> : (
                    <Button size="sm" variant="ghost" disabled={busy} aria-label={`重开“${action.title}”`}
                      onClick={() => onTransition(action, 'reopen')}>重开</Button>
                  )}
                </div>
              </li>
            ))}
          </ol>
        )}
      <form className="record-collaboration-composer record-action-composer vps-create-form__row" onSubmit={submit}>
        <Input label="行动标题" value={title} maxLength={512} required disabled={busy}
          onChange={(event) => setTitle(event.target.value)} />
        <Select label="指派给" value={assigneeId} disabled={busy} onChange={(event) => setAssigneeId(event.target.value)}>
          <option value="">暂不指派</option>
          {members.map((member) => <option key={member.id} value={member.id}>{member.label}</option>)}
        </Select>
        <Input label="截止时间" type="datetime-local" value={dueAt} disabled={busy}
          onChange={(event) => setDueAt(event.target.value)} />
        <Input label="关联修订" value={subjectRevisionId} disabled={busy}
          onChange={(event) => setSubjectRevisionId(event.target.value)} />
        <Button type="submit" disabled={busy || !title.trim()}>{editingAction ? '保存行动' : '新增行动'}</Button>
        {editingAction ? <Button variant="ghost" disabled={busy} onClick={resetComposer}>取消编辑</Button> : null}
      </form>
    </section>
  )
}

function toDateTimeLocal(value: string | null): string {
  if (value === null) return ''
  const date = new Date(value)
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
