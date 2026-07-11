import { Badge } from '../../../components/atoms'
import { formatDateTime } from '../../../lib/format'
import type {
  AssetDecisionFollowupStatus,
  AssetDecisionRecordMember,
} from '../../../lib/types'
import {
  ACTION_LABELS,
  FOLLOWUP_STATUS_LABELS,
  FOLLOWUP_STATUS_OPTIONS,
  ROLE_LABELS,
} from '../constants'
import {
  actionTone,
  compactDecisionText,
  compactMemberReadbackSummary,
  followupStatusTone,
  roleTone,
} from '../formatters'
import { previewItems, renderReadbackBadge } from '../renderHelpers'
import type { RecordFollowupDraft } from '../types'

type RecordFollowupRowsProps = {
  surface?: 'list' | 'cell'
  members: AssetDecisionRecordMember[]
  drafts: Readonly<Record<string, RecordFollowupDraft>>
  saving: Readonly<Record<string, boolean>>
  editingMemberID: string | null
  onUpdateDraft: (vpsID: string, patch: Partial<RecordFollowupDraft>) => void
  onEditMember: (vpsID: string | null) => void
  onSave: (member: AssetDecisionRecordMember) => void
  onShowRaw: () => void
}

type RecordFollowupEditorProps = Pick<
  RecordFollowupRowsProps,
  'drafts' | 'saving' | 'onUpdateDraft' | 'onEditMember' | 'onSave'
> & {
  member: AssetDecisionRecordMember
}

function RecordFollowupEditor({
  member,
  drafts,
  saving,
  onUpdateDraft,
  onEditMember,
  onSave,
}: RecordFollowupEditorProps) {
  const draft = drafts[member.vps_id] ?? {
    status: member.followup_status,
    note: member.followup_note,
  }
  const isSaving = Boolean(saving[member.vps_id])
  const isChanged = draft.status !== member.followup_status || draft.note !== member.followup_note

  return (
    <form
      className="asset-decision-followup-form"
      onSubmit={(event) => {
        event.preventDefault()
        onSave(member)
      }}
    >
      <div className="asset-decision-followup-form__status">
        <Badge variant="state" tone={followupStatusTone(member.followup_status)}>
          {FOLLOWUP_STATUS_LABELS[member.followup_status]}
        </Badge>
        <span>{member.followup_updated_at ? `更新 ${formatDateTime(member.followup_updated_at)}` : '尚未跟进'}</span>
      </div>
      <label className="visually-hidden" htmlFor={`followup-status-${member.record_id}-${member.vps_id}`}>
        {member.display_name || member.vps_id} 跟进状态
      </label>
      <select
        id={`followup-status-${member.record_id}-${member.vps_id}`}
        aria-label="跟进状态"
        className="input"
        value={draft.status}
        onChange={(event) => onUpdateDraft(member.vps_id, {
          status: event.target.value as AssetDecisionFollowupStatus,
        })}
      >
        {FOLLOWUP_STATUS_OPTIONS.map((option) => (
          <option key={option.value} value={option.value}>{option.label}</option>
        ))}
      </select>
      <label className="visually-hidden" htmlFor={`followup-note-${member.record_id}-${member.vps_id}`}>
        {member.display_name || member.vps_id} 跟进备注
      </label>
      <input
        id={`followup-note-${member.record_id}-${member.vps_id}`}
        aria-label="跟进备注"
        className="input"
        value={draft.note}
        placeholder="备注 / 阻塞原因"
        onChange={(event) => onUpdateDraft(member.vps_id, { note: event.target.value })}
      />
      <div className="asset-decision-followup-form__actions">
        <button className="btn sm primary" type="submit" disabled={isSaving || !isChanged}>
          {isSaving ? '保存中…' : '保存跟进'}
        </button>
        <button className="btn sm secondary" type="button" onClick={() => onEditMember(null)}>
          收起
        </button>
      </div>
    </form>
  )
}

export function RecordFollowupRows({
  surface = 'list',
  members,
  drafts,
  saving,
  editingMemberID,
  onUpdateDraft,
  onEditMember,
  onSave,
  onShowRaw,
}: RecordFollowupRowsProps) {
  if (surface === 'cell') {
    const member = members[0]
    if (!member) return null
    return editingMemberID === member.vps_id ? (
      <RecordFollowupEditor
        member={member}
        drafts={drafts}
        saving={saving}
        onUpdateDraft={onUpdateDraft}
        onEditMember={onEditMember}
        onSave={onSave}
      />
    ) : (
      <button className="btn sm primary" type="button" onClick={() => onEditMember(member.vps_id)}>
        编辑
      </button>
    )
  }

  const memberPreview = previewItems(members)
  if (members.length === 0) {
    return (
      <section className="asset-decision-record-followups" aria-label="成员跟进列表">
        <div className="asset-decision-member-decisions__empty">
          <strong>暂无成员跟进</strong>
          <span>当前记录没有可展示的成员。</span>
        </div>
      </section>
    )
  }

  return (
    <section className="asset-decision-record-followups" aria-label="成员跟进列表">
      {memberPreview.visible.map((member) => {
        const editing = editingMemberID === member.vps_id
        return (
          <article key={member.vps_id} className={`asset-decision-record-followup-row${editing ? ' asset-decision-record-followup-row--editing' : ''}`}>
            <div className="asset-decision-record-followup-row__identity">
              <strong>{member.display_name || member.vps_id}</strong>
              <span className="asset-decision-chip-row">
                <Badge variant="state" tone={roleTone(member.decided_role)}>
                  {ROLE_LABELS[member.decided_role]}
                </Badge>
                <Badge variant="state" tone={actionTone(member.decided_action)}>
                  {ACTION_LABELS[member.decided_action]}
                </Badge>
              </span>
            </div>
            <div className="asset-decision-record-followup-row__state">
              <span className="asset-decision-chip-row">
                {renderReadbackBadge(member.execution_readback)}
                <Badge variant="state" tone={followupStatusTone(member.followup_status)}>
                  {FOLLOWUP_STATUS_LABELS[member.followup_status]}
                </Badge>
              </span>
              <strong>
                {compactDecisionText(member.followup_note || compactMemberReadbackSummary(member.execution_readback), '尚未跟进')}
              </strong>
            </div>
            <div className="asset-decision-record-followup-row__form">
              {editing ? (
                <RecordFollowupEditor
                  member={member}
                  drafts={drafts}
                  saving={saving}
                  onUpdateDraft={onUpdateDraft}
                  onEditMember={onEditMember}
                  onSave={onSave}
                />
              ) : (
                <button
                  className="btn-text sm secondary"
                  type="button"
                  aria-label="编辑跟进"
                  aria-expanded={false}
                  onClick={() => onEditMember(member.vps_id)}
                >
                  编辑跟进
                </button>
              )}
            </div>
          </article>
        )
      })}
      {memberPreview.hiddenCount > 0 && (
        <div className="asset-decision-preview-more" role="note">
          <span>另有 {memberPreview.hiddenCount} 台在成员底稿中查看</span>
          <button className="btn-text sm secondary" type="button" onClick={onShowRaw}>
            查看成员底稿
          </button>
        </div>
      )}
    </section>
  )
}
