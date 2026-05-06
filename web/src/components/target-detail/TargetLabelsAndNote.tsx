import type { FormEvent } from 'react'

import { DetailSection } from '../DetailSection'
import { formatLabelList } from '../../lib/format'
import type { TargetRecord } from '../../lib/types'

type TargetLabelsAndNoteProps = {
  target: TargetRecord
  editing: boolean
  groupDraft: string
  labelDraft: string
  noteDraft: string
  submitting: boolean
  error: string | null
  onGroupDraftChange: (value: string) => void
  onLabelDraftChange: (value: string) => void
  onNoteDraftChange: (value: string) => void
  onStartEdit: () => void
  onCancelEdit: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function TargetLabelsAndNote({
  target,
  editing,
  groupDraft,
  labelDraft,
  noteDraft,
  submitting,
  error,
  onGroupDraftChange,
  onLabelDraftChange,
  onNoteDraftChange,
  onStartEdit,
  onCancelEdit,
  onSubmit,
}: TargetLabelsAndNoteProps) {
  return (
    <DetailSection eyebrow="标签与备注" title="标签与备注">
      <div className="page-stack">
        {editing ? (
          <form onSubmit={onSubmit} className="page-stack">
            <label>
              Group
              <input
                name="metadata-group"
                value={groupDraft}
                onChange={(event) => onGroupDraftChange(event.target.value)}
              />
            </label>
            <label>
              标签
              <input
                name="metadata-labels"
                value={labelDraft}
                onChange={(event) => onLabelDraftChange(event.target.value)}
              />
            </label>
            <label>
              备注
              <textarea
                name="metadata-note"
                value={noteDraft}
                onChange={(event) => onNoteDraftChange(event.target.value)}
                rows={3}
              />
            </label>
            <div className="badge-row badge-row--wrap">
              <button type="submit" disabled={submitting}>
                {submitting ? '正在保存…' : '保存标签与备注'}
              </button>
              <button
                type="button"
                disabled={submitting}
                onClick={() => onCancelEdit()}
              >
                取消
              </button>
            </div>
          </form>
        ) : (
          <>
            {target.group ? <p>Group：{target.group}</p> : null}
            <p>标签：{formatLabelList(target.labels)}</p>
            <p>备注：{target.note.trim() || '暂无备注'}</p>
            <div>
              <button type="button" onClick={() => onStartEdit()}>
                编辑标签与备注
              </button>
            </div>
          </>
        )}
        {error ? (
          <p role="alert" aria-live="assertive">
            {error}
          </p>
        ) : null}
      </div>
    </DetailSection>
  )
}
