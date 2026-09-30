import type { RecordDraftPayload, RecordRevision } from '../../../lib/types'
import { differingRecordFields, recordComparablePayload } from '../recordFields'

type RecordSaveImpactProps = {
  baseline?: RecordRevision | null
  payload: RecordDraftPayload
}

// Every publish carries its own save reason, so treating it as a changed field would
// report an impact on a record whose content is untouched.
const IGNORED_FIELDS = new Set(['save_reason'])

export function RecordSaveImpact({ baseline, payload }: RecordSaveImpactProps) {
  const changes = describeSaveImpact(baseline, payload)
  return (
    <div className="record-save-impact" role="group" aria-label="保存影响">
      {changes.length === 0 ? <p className="record-muted">正式修订字段尚未变化</p> : (
        <ul className="record-save-impact__list">
          {changes.map((change) => <li key={change}>{change}</li>)}
        </ul>
      )}
    </div>
  )
}

// eslint-disable-next-line react-refresh/only-export-components -- save-impact helper colocated with the panel
export function describeSaveImpact(baseline: RecordRevision | null | undefined, payload: RecordDraftPayload): string[] {
  if (!baseline) {
    return payload.title.trim() ? ['将创建首个正式修订'] : []
  }
  return differingRecordFields(recordComparablePayload(baseline), payload)
    .filter(({ field }) => !IGNORED_FIELDS.has(field))
    .map(({ label }) => `${label}将写入新修订`)
}
