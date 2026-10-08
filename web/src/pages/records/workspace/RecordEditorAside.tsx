import { Button, Input, Select } from '../../../components/atoms'
import { RecordVisibilityFields } from './RecordVisibilityFields'
import { RecordRevisionCollaborationControls } from '../../../components/RecordRevisionCollaborationControls'
import type { RecordCollaborationMemberOption } from '../../../components/RecordRevisionCollaborationControls'
import type { RecordBusinessStatus, RecordDraftPayload, RecordRevision, RecordType } from '../../../lib/types'
import type { RecordMaterialItem } from '../editor/RecordMaterialDrawer'
import { RecordMaterialList } from '../editor/RecordMaterialList'
import { RecordOutline } from '../editor/RecordOutline'
import { RecordSaveImpact } from '../editor/RecordSaveImpact'
import { labelOptions, RECORD_SUBJECT_KIND_LABELS, RECORD_TYPE_LABELS } from '../recordLabels'
import {
  applyRecordTypeChange,
  BUSINESS_STATUS_LABELS,
  businessStatusesForType,
  patchPrimarySubject,
  typeSupportsBusinessStatus,
} from '../recordWorkspaceModel'
import { countClass } from './recordPresentation'

type RecordEditorAsideProps = {
  payload: RecordDraftPayload
  baseline: RecordRevision | null
  members: readonly RecordCollaborationMemberOption[]
  materials: readonly RecordMaterialItem[]
  onPatch: (patch: Partial<RecordDraftPayload>) => void
  onOpenMaterials: () => void
}

export function RecordEditorAside({ payload, baseline, members, materials, onPatch, onOpenMaterials }: RecordEditorAsideProps) {
  const subject = payload.subjects[0]
  return (
    <>
      <section className="record-section" aria-labelledby="record-attributes-title">
        <h2 className="record-section__title" id="record-attributes-title">属性</h2>
        <div className="record-form-grid">
          <Select
            label="记录类型"
            value={payload.record_type}
            onChange={(event) => onPatch(applyRecordTypeChange(event.target.value as RecordType))}
          >
            {labelOptions(RECORD_TYPE_LABELS).map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </Select>
          {typeSupportsBusinessStatus(payload.record_type) ? (
            <Select
              label="业务状态"
              value={payload.business_status}
              onChange={(event) => onPatch({ business_status: event.target.value as RecordBusinessStatus })}
            >
              {businessStatusesForType(payload.record_type).map((status) => (
                <option key={status} value={status}>{BUSINESS_STATUS_LABELS[status]}</option>
              ))}
            </Select>
          ) : null}
          <Input label="影响级别" value={payload.impact_level} onChange={(event) => onPatch({ impact_level: event.target.value })} />
          <RecordVisibilityFields
            visibility={payload.visibility}
            onChange={(visibility) => onPatch({ visibility })}
          />
          <div className="record-form-grid__wide">
            <Input
              label="主体 ID"
              value={subject?.source_id ?? ''}
              {...(subject ? { hint: RECORD_SUBJECT_KIND_LABELS[subject.kind] } : {})}
              onChange={(event) => onPatch({ subjects: patchPrimarySubject(payload.subjects, event.target.value) })}
            />
          </div>
        </div>
      </section>

      <RecordRevisionCollaborationControls
        state="ready"
        members={members}
        ownerId={payload.owner_id}
        participantIds={payload.participant_ids}
        followUpAt={toDateTimeLocal(payload.follow_up_at)}
        onOwnerChange={(ownerId) => onPatch({ owner_id: ownerId })}
        onParticipantToggle={(participantId, selected) => onPatch({
          participant_ids: selected
            ? [...new Set([...payload.participant_ids, participantId])]
            : payload.participant_ids.filter((id) => id !== participantId),
        })}
        onFollowUpChange={(followUpAt) => onPatch({ follow_up_at: followUpAt ? new Date(followUpAt).toISOString() : null })}
      />

      <section className="record-section" aria-labelledby="record-publish-title">
        <h2 className="record-section__title" id="record-publish-title">发布</h2>
        <Input label="保存原因" value={payload.save_reason} onChange={(event) => onPatch({ save_reason: event.target.value })} />
        <RecordSaveImpact baseline={baseline} payload={payload} />
      </section>

      <section className="record-section" aria-labelledby="record-materials-title">
        <div className="record-section__head">
          <h2 className="record-section__title" id="record-materials-title">
            材料 <span className={countClass(materials.length)}>{materials.length}</span>
          </h2>
          <Button size="sm" variant="secondary" onClick={onOpenMaterials}>管理材料</Button>
        </div>
        {materials.length > 0 ? <RecordMaterialList items={materials} /> : null}
      </section>

      <RecordOutline source={payload.body_markdown} minHeadings={1} />
    </>
  )
}

function toDateTimeLocal(value?: string | null): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
