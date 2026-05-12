import { Button } from '../atoms/Button'
import { formatLabelList } from '../../lib/format'
import type { NodeRecord } from '../../lib/types'

type NodeLabelsAndNoteProps = {
  node: NodeRecord
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
  onSave: () => void
}

export function NodeLabelsAndNote({
  node,
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
  onSave,
}: NodeLabelsAndNoteProps) {
  return (
    <div className="watchtower-property-item" style={{ flexDirection: editing ? 'column' : 'row', alignItems: editing ? 'stretch' : 'center' }}>
      {editing ? (
        <div className="page-stack" style={{ width: '100%' }}>
          <p>
            <label>
              Group
              <input
                name="metadata-group"
                value={groupDraft}
                onChange={(event) => onGroupDraftChange(event.target.value)}
              />
            </label>
          </p>
          <p>
            <label>
              标签
              <input
                name="metadata-labels"
                value={labelDraft}
                onChange={(event) => onLabelDraftChange(event.target.value)}
              />
            </label>
          </p>
          <p>
            <label>
              备注
              <textarea
                name="metadata-note"
                value={noteDraft}
                onChange={(event) => onNoteDraftChange(event.target.value)}
                rows={3}
              />
            </label>
          </p>
          <div className="badge-row badge-row--wrap">
            <Button
              variant="primary"
              disabled={submitting}
              onClick={() => onSave()}
            >
              {submitting ? '正在保存…' : '保存标签与备注'}
            </Button>
            <Button
              variant="ghost"
              disabled={submitting}
              onClick={() => onCancelEdit()}
            >
              取消
            </Button>
          </div>
          {error ? <p role="alert" style={{ color: 'var(--color-state-critical)' }}>{error}</p> : null}
        </div>
      ) : (
        <>
          <div className="watchtower-property-item__main">
            <h3 className="watchtower-property-item__title">标签与备注</h3>
            {node.group ? (
              <span className="watchtower-property-item__desc">Group：{node.group}</span>
            ) : null}
            <span className="watchtower-property-item__desc">
              标签：{formatLabelList(node.labels)}
            </span>
            <span className="watchtower-property-item__desc">
              备注：{node.note.trim() || '暂无备注'}
            </span>
            {error ? <span role="alert" style={{ color: 'var(--color-state-critical)' }}>{error}</span> : null}
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
