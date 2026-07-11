import { Badge } from '../../../components/atoms'
import type {
  AssetDecisionEvidenceChip,
  AssetDecisionSuggestedAction,
  AssetDecisionSuggestedRole,
} from '../../../lib/types'
import {
  ACTION_LABELS,
  ACTION_OPTIONS,
  ROLE_LABELS,
  ROLE_OPTIONS,
} from '../constants'
import { actionTone, roleTone } from '../formatters'
import { previewItems } from '../renderHelpers'
import type { RecordDraft, RecordMemberDraft } from '../types'

export type RecordDraftMemberRow = Readonly<{
  vpsID: string
  displayName: string
  fallbackRole: AssetDecisionSuggestedRole
  fallbackAction: AssetDecisionSuggestedAction
  meta?: string
  chips?: AssetDecisionEvidenceChip[]
}>

type RecordDraftMemberRowsProps = {
  members: RecordDraftMemberRow[]
  draft: Readonly<RecordDraft>
  editingMemberID: string | null
  onEditMember: (vpsID: string | null) => void
  onUpdateMember: (vpsID: string, patch: Partial<RecordMemberDraft>) => void
}

export function RecordDraftMemberRows({
  members,
  draft,
  editingMemberID,
  onEditMember,
  onUpdateMember,
}: RecordDraftMemberRowsProps) {
  const memberPreview = previewItems(members)

  return (
    <div className="asset-decision-save-members" aria-label="保存记录成员复核">
      {memberPreview.visible.map((member) => {
        const memberDraft = draft.members[member.vpsID]
        const decidedRole = memberDraft?.decidedRole ?? member.fallbackRole
        const decidedAction = memberDraft?.decidedAction ?? member.fallbackAction
        const reason = memberDraft?.reason ?? ''
        const editing = editingMemberID === member.vpsID
        return (
          <article key={member.vpsID} className={`asset-decision-save-member${editing ? ' asset-decision-save-member--editing' : ''}`}>
            <div className="asset-decision-save-member__summary">
              <div className="asset-table__identity">
                <strong>{member.displayName}</strong>
              </div>
              <span className="asset-decision-chip-row">
                <Badge variant="state" tone={roleTone(decidedRole)}>
                  {ROLE_LABELS[decidedRole]}
                </Badge>
                <Badge variant="state" tone={actionTone(decidedAction)}>
                  {ACTION_LABELS[decidedAction]}
                </Badge>
                <Badge variant="state" tone={reason.trim() ? 'normal' : 'maintenance'}>
                  {reason.trim() ? '已写理由' : '理由待补'}
                </Badge>
              </span>
              <button
                className="btn-text sm secondary"
                type="button"
                aria-expanded={editing}
                onClick={() => onEditMember(editing ? null : member.vpsID)}
              >
                {editing ? '收起编辑' : `编辑 ${member.displayName} 成员理由`}
              </button>
            </div>
            {editing && (
              <div className="asset-decision-save-member__editor">
                <div className="input-field">
                  <span>角色</span>
                  <select
                    aria-label="角色"
                    className="input"
                    value={decidedRole}
                    onChange={(event) => onUpdateMember(member.vpsID, {
                      decidedRole: event.target.value as AssetDecisionSuggestedRole,
                    })}
                  >
                    {ROLE_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </div>
                <div className="input-field">
                  <span>动作</span>
                  <select
                    aria-label="动作"
                    className="input"
                    value={decidedAction}
                    onChange={(event) => onUpdateMember(member.vpsID, {
                      decidedAction: event.target.value as AssetDecisionSuggestedAction,
                    })}
                  >
                    {ACTION_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </div>
                <div className="input-field asset-decision-save-member__reason">
                  <span>理由</span>
                  <input
                    aria-label="理由"
                    className="input"
                    value={reason}
                    onChange={(event) => onUpdateMember(member.vpsID, { reason: event.target.value })}
                  />
                </div>
              </div>
            )}
          </article>
        )
      })}
      {memberPreview.hiddenCount > 0 && (
        <div className="asset-decision-preview-more" role="note">
          另有 {memberPreview.hiddenCount} 台成员保留在保存底稿中
        </div>
      )}
    </div>
  )
}
