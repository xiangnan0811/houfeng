import { StrictMode, useState } from 'react'
import { createRoot } from 'react-dom/client'

import '../../src/styles/reset.css'
import '../../src/styles/tokens.css'
import '../../src/index.css'
import '../../src/styles/modernize.css'

import { RecordActionPanel } from '../../src/components/RecordActionPanel'
import { RecordCommentThread } from '../../src/components/RecordCommentThread'
import { RecordRevisionCollaborationControls } from '../../src/components/RecordRevisionCollaborationControls'
import { RecordWatchControl } from '../../src/components/RecordWatchControl'
import type { RecordCollaborationSurfaceState } from '../../src/components/RecordCollaborationState'
import type { RecordAction, RecordComment, RecordWatch } from '../../src/lib/types'

const members = [
  { id: 'usr_0123456789abcdef01234567', label: '林岚' },
  { id: 'usr_89abcdef0123456701234567', label: '周衡' },
]

const baseAction: RecordAction = {
  action_id: 'ract_browser1', record_id: 'rec_browser1', version: 2, status: 'open', title: '复核异常证据',
	details: '在授权协作面板中保留的排查步骤。',
  assignee_id: members[1]!.id, due_at: '2026-08-20T09:00:00Z', completed_at: null,
  subject_revision_id: 'rrv_browser1', created_at: '2026-08-17T09:00:00Z', updated_at: '2026-08-17T10:00:00Z',
}

const baseComment: RecordComment = {
  comment_id: 'rcm_browser1', record_id: 'rec_browser1', author_id: members[0]!.id, version: 2, state: 'active',
  body_markdown: '**已验证**', render_model: { version: 'comment_markdown/v1', nodes: [{
    type: 'paragraph', children: [{ type: 'strong', children: [{ type: 'text', text: '已验证' }] }],
  }] }, reply_to_comment_id: '', mention_user_ids: [], created_at: '2026-08-17T09:00:00Z',
  updated_at: '2026-08-17T10:00:00Z', redacted_at: null,
}

const baseWatch: RecordWatch = {
  record_id: 'rec_browser1', user_id: members[0]!.id, version: 3, preference: 'default',
  sources: { author: false, owner: true, participant: false, comment: true, mention: false, action: false },
  updated_at: '2026-08-17T10:00:00Z',
}

function requestedState(): RecordCollaborationSurfaceState {
  const value = new URLSearchParams(window.location.search).get('state')
  return ['ready', 'loading', 'empty', 'error', 'revoked', 'deleted'].includes(value ?? '')
    ? value as RecordCollaborationSurfaceState : 'ready'
}

export function Harness() {
  const state = requestedState()
  const [ownerId, setOwnerId] = useState(members[0]!.id)
  const [participantIds, setParticipantIds] = useState<string[]>([members[1]!.id])
  const [followUpAt, setFollowUpAt] = useState('2026-08-20T09:30')
  const [actions, setActions] = useState<RecordAction[]>([baseAction])
  const [comments, setComments] = useState<RecordComment[]>([baseComment])
  const [watch, setWatch] = useState<RecordWatch>(baseWatch)
  const [lastEvent, setLastEvent] = useState('就绪')
  const ready = state === 'ready'

  return (
    <main className="page-stack" id="component-harness">
      <div className="page-header">
        <div><div className="page-eyebrow">TEST-ONLY COMPONENT HARNESS</div><h1 className="page-title">记录协作值班台</h1></div>
        <span role="status" className="badge badge--info">{lastEvent}</span>
      </div>
      <RecordRevisionCollaborationControls state={state} members={ready ? members : []}
        ownerId={ownerId} participantIds={participantIds} followUpAt={followUpAt}
        onOwnerChange={(value) => { setOwnerId(value); setLastEvent(`owner:${value}`) }}
        onParticipantToggle={(value, selected) => {
          setParticipantIds((current) => selected ? [...new Set([...current, value])] : current.filter((id) => id !== value))
          setLastEvent(`participant:${value}:${selected}`)
        }}
        onFollowUpChange={(value) => { setFollowUpAt(value); setLastEvent(`follow-up:${value}`) }} />
      <RecordActionPanel state={state} actions={ready ? actions : []} members={ready ? members : []} busy={false}
        onCreate={(values) => setLastEvent(`action:create:${values.title}`)}
        onUpdate={(action, values) => {
          setActions((current) => current.map((item) => item.action_id === action.action_id
            ? { ...item, ...values, version: item.version + 1 } : item))
          setLastEvent(`action:update:${values.version}`)
        }}
        onTransition={(_action, transition) => setLastEvent(`action:${transition}`)} />
      <RecordCommentThread state={state} comments={ready ? comments : []} currentUserId={members[0]!.id}
        members={ready ? members : []} busy={false}
        onSubmit={(input) => setLastEvent(`comment:${input.mode}:${input.version}`)}
        onRedact={(comment) => {
          setComments((current) => current.map((item) => item.comment_id === comment.comment_id
            ? { ...item, state: 'redacted', body_markdown: null, render_model: null, mention_user_ids: [], redacted_at: '2026-08-17T11:00:00Z' } : item))
          setLastEvent(`comment:redact:${comment.version}`)
        }} />
      <RecordWatchControl state={state} watch={ready ? watch : null} busy={false}
        onChange={(preference) => {
          setWatch((current) => ({ ...current, preference, version: current.version + 1 }))
          setLastEvent(`watch:${preference}`)
        }} />
    </main>
  )
}

createRoot(document.getElementById('root')!).render(<StrictMode><Harness /></StrictMode>)
