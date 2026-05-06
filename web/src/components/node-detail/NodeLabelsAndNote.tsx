import { DetailSection } from '../DetailSection'
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
    <DetailSection eyebrow="标签与备注" title="标签与备注">
      <div className="page-stack">
        {editing ? (
          <>
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
              <button
                type="button"
                disabled={submitting}
                onClick={() => onSave()}
              >
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
          </>
        ) : (
          <>
            {node.group ? <p>Group：{node.group}</p> : null}
            <p>标签：{formatLabelList(node.labels)}</p>
            <p>备注：{node.note.trim() || '暂无备注'}</p>
            <div>
              <button type="button" onClick={() => onStartEdit()}>
                编辑标签与备注
              </button>
            </div>
          </>
        )}
        {error ? <p role="alert">{error}</p> : null}
      </div>
    </DetailSection>
  )
}
