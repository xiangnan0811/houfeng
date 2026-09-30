import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'
import { Input, Select } from './atoms'

export type RecordCollaborationMemberOption = {
  id: string
  label: string
}

type RecordRevisionCollaborationControlsProps = {
  state: RecordCollaborationSurfaceState
  members: readonly RecordCollaborationMemberOption[]
  ownerId: string
  participantIds: readonly string[]
  followUpAt: string
  disabled?: boolean
  onOwnerChange: (ownerId: string) => void
  onParticipantToggle: (participantId: string, selected: boolean) => void
  onFollowUpChange: (followUpAt: string) => void
}

export function RecordRevisionCollaborationControls({
  state,
  members,
  ownerId,
  participantIds,
  followUpAt,
  disabled = false,
  onOwnerChange,
  onParticipantToggle,
  onFollowUpChange,
}: RecordRevisionCollaborationControlsProps) {
  if (state !== 'ready') {
    return <RecordCollaborationState state={state} loadingTitle="正在读取协作字段" emptyTitle="暂无协作字段" errorTitle="协作字段暂不可用" />
  }
  return (
    <section className="record-collaboration-panel record-collaboration-panel--revision" aria-labelledby="record-revision-collaboration-title">
      <header className="record-collaboration-panel__header">
        <h2 className="record-collaboration-panel__title" id="record-revision-collaboration-title">协作</h2>
      </header>
      <div className="record-collaboration-grid">
        <Select label="负责人" value={ownerId} disabled={disabled} onChange={(event) => onOwnerChange(event.target.value)}>
          <option value="">未指定</option>
          {members.map((member) => <option key={member.id} value={member.id}>{member.label}</option>)}
        </Select>
        <Input label="跟进时间" type="datetime-local" value={followUpAt} disabled={disabled}
          onChange={(event) => onFollowUpChange(event.target.value)} />
      </div>
      <fieldset className="record-collaboration-members" disabled={disabled}>
        <legend className="input-field__label">参与人</legend>
        {members.map((member) => (
          <label key={member.id} className="record-chip">
            <input type="checkbox" checked={participantIds.includes(member.id)}
              onChange={(event) => onParticipantToggle(member.id, event.target.checked)} />
            <span>{member.label}</span>
          </label>
        ))}
      </fieldset>
    </section>
  )
}
