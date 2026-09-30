import { useEffect, useRef, useState, type FormEvent } from 'react'

import type { RecordAction, RecordActionStatus, RecordActionTransition } from '../lib/types'
import { formatDateTime } from '../lib/format'
import type { RecordCollaborationMemberOption } from './RecordRevisionCollaborationControls'
import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'
import { Button, Input, Select } from './atoms'

export type RecordActionCreateValues = {
  title: string
	details: string
  assignee_id: string
  due_at: string | null
  subject_revision_id: string
}

export type RecordActionUpdateValues = {
  title: string
	details: string
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
	if (state === 'loading' || state === 'error' || state === 'revoked' || state === 'deleted') {
		return <RecordCollaborationState state={state} loadingTitle="正在读取行动项" emptyTitle="暂无行动项" errorTitle="行动项暂不可用" />
	}
	const freshStateKey = `${state}:${actions.map((action) => `${action.action_id}:${action.version}:${action.status}`).join(',')}`
	return <ReadyRecordActionPanel key={freshStateKey} state={state} actions={actions} members={members} busy={busy}
		onCreate={onCreate} onUpdate={onUpdate} onTransition={onTransition} />
}

const STATUS_LABELS: Record<RecordActionStatus, string> = {
  open: '进行中',
  completed: '已完成',
  cancelled: '已取消',
}

const STATUS_CLASSES: Record<RecordActionStatus, string> = {
  open: 'record-action record-action--open',
  completed: 'record-action record-action--completed',
  cancelled: 'record-action record-action--cancelled',
}

function ReadyRecordActionPanel({ state, actions, members, busy, onCreate, onUpdate, onTransition }: RecordActionPanelProps) {
  const [composing, setComposing] = useState(false)
  const [title, setTitle] = useState('')
  const [details, setDetails] = useState('')
  const [assigneeId, setAssigneeId] = useState('')
  const [dueAt, setDueAt] = useState('')
  const [subjectRevisionId, setSubjectRevisionId] = useState('')
  const [editingActionId, setEditingActionId] = useState('')
  const editingAction = actions.find((action) => action.action_id === editingActionId) ?? null
  const composerOpen = composing || editingAction !== null
  const titleRef = useRef<HTMLInputElement>(null)
  const addRef = useRef<HTMLButtonElement>(null)
  // 表单按需展开：打开后把焦点交给标题，取消后还给"新增行动"，键盘操作不丢焦点。
  const pendingFocus = useRef<'title' | 'add' | null>(null)
  useEffect(() => {
    const target = pendingFocus.current
    pendingFocus.current = null
    if (target === 'title') titleRef.current?.focus()
    if (target === 'add') addRef.current?.focus()
  })
  const memberLabel = (id: string) => members.find((member) => member.id === id)?.label ?? id

  function submit(event: FormEvent) {
    event.preventDefault()
    const normalizedTitle = title.trim()
    if (!normalizedTitle || busy) return
    const values = {
      title: normalizedTitle,
      details,
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
    })
  }

  function beginEdit(action: RecordAction) {
    pendingFocus.current = 'title'
    setEditingActionId(action.action_id)
    setTitle(action.title)
    setDetails(action.details)
    setAssigneeId(action.assignee_id)
    setDueAt(toDateTimeLocal(action.due_at))
    setSubjectRevisionId(action.subject_revision_id)
  }

  function resetComposer() {
    pendingFocus.current = 'add'
    setComposing(false)
    setEditingActionId('')
    setTitle('')
    setDetails('')
    setAssigneeId('')
    setDueAt('')
    setSubjectRevisionId('')
  }

  const openCount = actions.filter((action) => action.status === 'open').length

  return (
    <section className="record-collaboration-panel record-action-panel" aria-labelledby="record-actions-title">
      <header className="record-collaboration-panel__header">
        <h2 className="record-collaboration-panel__title" id="record-actions-title">
          行动项
          <span className={openCount > 0 ? 'record-count record-count--active' : 'record-count'}>{actions.length}</span>
        </h2>
        {composerOpen ? null : (
          <Button ref={addRef} size="sm" variant="secondary" disabled={busy} onClick={() => {
            pendingFocus.current = 'title'
            setComposing(true)
          }}>新增行动</Button>
        )}
      </header>
      {state === 'empty' || actions.length === 0
        ? (composerOpen ? null : <p className="record-collaboration-panel__empty">暂无行动项</p>)
        : (
          <ol className="record-action-list">
            {actions.map((action) => (
              <li key={action.action_id} className={STATUS_CLASSES[action.status]}>
                <div className="record-action__body">
                  <strong className="record-action__title">{action.title}</strong>
                  {action.details ? <p className="record-action__details">{action.details}</p> : null}
                  <span className="record-action__meta">
                    {action.status === 'open' ? null : <span className="record-action__status">{STATUS_LABELS[action.status]}</span>}
                    <span>{action.assignee_id ? memberLabel(action.assignee_id) : '未指派'}</span>
                    <span>{action.due_at ? `截止 ${formatDateTime(action.due_at)}` : '无截止时间'}</span>
                  </span>
                </div>
                <div className="record-action__commands">
                  {action.status === 'open' ? <>
                    <Button size="sm" variant="secondary" disabled={busy} aria-label={`完成“${action.title}”`}
                      onClick={() => onTransition(action, 'complete')}>完成</Button>
                    <Button size="sm" variant="ghost" disabled={busy} aria-label={`取消“${action.title}”`}
                      onClick={() => onTransition(action, 'cancel')}>取消</Button>
                  </> : (
                    <Button size="sm" variant="ghost" disabled={busy} aria-label={`重开“${action.title}”`}
                      onClick={() => onTransition(action, 'reopen')}>重开</Button>
                  )}
                  <Button size="sm" variant="ghost" disabled={busy} aria-label={`编辑“${action.title}”`}
                    onClick={() => beginEdit(action)}>编辑</Button>
                </div>
              </li>
            ))}
          </ol>
        )}
      {composerOpen ? (
        <form className="record-collaboration-composer record-action-composer" onSubmit={submit}
          aria-label={editingAction ? '编辑行动' : '新增行动'}>
          <Input ref={titleRef} label="行动标题" value={title} maxLength={512} required disabled={busy}
            onChange={(event) => setTitle(event.target.value)} />
          <div className="input-field">
            <label className="input-field__label" htmlFor="record-action-details">行动详情</label>
            <textarea id="record-action-details" className="input record-action-composer__details" rows={3} value={details}
              maxLength={4_096} disabled={busy} onChange={(event) => setDetails(event.target.value)} />
          </div>
          <div className="record-action-composer__row">
            <Select label="指派给" value={assigneeId} disabled={busy} onChange={(event) => setAssigneeId(event.target.value)}>
              <option value="">暂不指派</option>
              {members.map((member) => <option key={member.id} value={member.id}>{member.label}</option>)}
            </Select>
            <Input label="截止时间" type="datetime-local" value={dueAt} disabled={busy}
              onChange={(event) => setDueAt(event.target.value)} />
            <Input label="关联修订" value={subjectRevisionId} disabled={busy}
              onChange={(event) => setSubjectRevisionId(event.target.value)} />
          </div>
          <div className="record-collaboration-composer__commands">
            <Button size="sm" variant="ghost" disabled={busy} onClick={resetComposer}>取消</Button>
            <Button size="sm" type="submit" disabled={busy || !title.trim()}>{editingAction ? '保存行动' : '添加行动'}</Button>
          </div>
        </form>
      ) : null}
    </section>
  )
}

function toDateTimeLocal(value: string | null): string {
  if (value === null) return ''
  const date = new Date(value)
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
