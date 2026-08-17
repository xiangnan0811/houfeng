import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'
import { Input, Select } from './atoms'

export type RecordCollaborationMemberOption = {
  id: string
  label: string
}

type RecordRevisionCollaborationControlsProps = {
  state: Exclude<RecordCollaborationSurfaceState, 'empty'>
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
    <section className="record-collaboration-panel record-collaboration-panel--revision card" aria-labelledby="record-revision-collaboration-title">
      <header className="record-collaboration-panel__header section-heading">
        <div>
          <p className="record-collaboration-panel__eyebrow section-heading__eyebrow">REVISION CONTROL</p>
          <h2 className="section-heading__title" id="record-revision-collaboration-title">协作责任</h2>
        </div>
        <span className="record-collaboration-panel__signal badge badge--info">随完整修订保存</span>
      </header>
      <div className="record-collaboration-grid vps-create-form__row">
        <Select label="负责人" value={ownerId} disabled={disabled} onChange={(event) => onOwnerChange(event.target.value)}>
          <option value="">未指定</option>
          {members.map((member) => <option key={member.id} value={member.id}>{member.label}</option>)}
        </Select>
        <Input label="跟进时间" type="datetime-local" value={followUpAt} disabled={disabled}
          onChange={(event) => onFollowUpChange(event.target.value)} />
      </div>
      <fieldset className="record-collaboration-members" disabled={disabled}>
        <legend>参与人</legend>
        <div className="record-collaboration-members__list asset-option-grid">
          {members.map((member) => (
            <label key={member.id} className="record-collaboration-member asset-option-radio">
              <input type="checkbox" checked={participantIds.includes(member.id)}
                onChange={(event) => onParticipantToggle(member.id, event.target.checked)} />
              <span>{member.label}</span>
              <code aria-hidden="true">{member.id}</code>
            </label>
          ))}
        </div>
      </fieldset>
    </section>
  )
}
