import type { FormEvent } from 'react'

import { Button } from '../atoms/Button'
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
    <div className="watchtower-property-item" style={{ flexDirection: editing ? 'column' : 'row', alignItems: editing ? 'stretch' : 'center' }}>
      {editing ? (
        <form onSubmit={onSubmit} className="page-stack" style={{ width: '100%' }}>
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
            <Button type="submit" variant="primary" disabled={submitting}>
              {submitting ? '正在保存…' : '保存标签与备注'}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={submitting}
              onClick={() => onCancelEdit()}
            >
              取消
            </Button>
          </div>
          {error ? (
            <p role="alert" aria-live="assertive" style={{ color: 'var(--color-state-critical)' }}>
              {error}
            </p>
          ) : null}
        </form>
      ) : (
        <>
          <div className="watchtower-property-item__main">
            <h3 className="watchtower-property-item__title">标签与备注</h3>
            {target.group ? (
              <span className="watchtower-property-item__desc">Group：{target.group}</span>
            ) : null}
            <span className="watchtower-property-item__desc">
              标签：{formatLabelList(target.labels)}
            </span>
            <span className="watchtower-property-item__desc">
              备注：{target.note.trim() || '暂无备注'}
            </span>
            {error ? (
              <span role="alert" aria-live="assertive" style={{ color: 'var(--color-state-critical)' }}>
                {error}
              </span>
            ) : null}
          </div>
          <div className="watchtower-property-item__actions">
            <Button variant="secondary" onClick={() => onStartEdit()}>
              编辑标签与备注
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
