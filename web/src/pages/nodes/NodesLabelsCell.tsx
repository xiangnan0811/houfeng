import { Button } from '../../components/atoms'
import type { NodeRecord } from '../../lib/types'

type NodesLabelsCellProps = {
  node: NodeRecord
  editing: boolean
  labelDraft: string
  groupDraft: string
  metadataBusyNodeId: string | null
  metadataError?: string
  onLabelDraftChange: (value: string) => void
  onGroupDraftChange: (value: string) => void
  onSaveLabels: (node: NodeRecord) => void
  onCancelLabels: (node: NodeRecord) => void
}

function renderLabelsCell(node: NodeRecord) {
  if (node.labels.length === 0) return <span className="empty-inline">—</span>
  const visible = node.labels.slice(0, 3)
  const overflow = node.labels.length - visible.length
  return (
    <span className="nodes-table__labels">
      {visible.join(' · ')}
      {overflow > 0 ? (
        <span className="nodes-table__labels-more"> +{overflow}</span>
      ) : null}
    </span>
  )
}

export function NodesLabelsCell({
  node,
  editing,
  labelDraft,
  groupDraft,
  metadataBusyNodeId,
  metadataError,
  onLabelDraftChange,
  onGroupDraftChange,
  onSaveLabels,
  onCancelLabels,
}: NodesLabelsCellProps) {
  if (!editing) return renderLabelsCell(node)

  return (
    <div
      className="nodes-table__label-editor"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.stopPropagation()
        }
      }}
    >
      <label className="nodes-table__label-editor-field">
        <span className="visually-hidden">Group</span>
        <input
          name={`group-${node.node_id}`}
          value={groupDraft}
          onChange={(event) => onGroupDraftChange(event.target.value)}
          aria-label="Group"
          placeholder="Group"
        />
      </label>
      <label className="nodes-table__label-editor-field">
        <span className="visually-hidden">标签</span>
        <input
          name={`labels-${node.node_id}`}
          value={labelDraft}
          onChange={(event) => onLabelDraftChange(event.target.value)}
          aria-label="标签"
        />
      </label>
      <div className="nodes-table__label-editor-actions">
        <Button
          size="sm"
          variant="primary"
          disabled={metadataBusyNodeId === node.node_id}
          onClick={() => onSaveLabels(node)}
        >
          {metadataBusyNodeId === node.node_id ? '正在保存…' : '保存标签'}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={metadataBusyNodeId === node.node_id}
          onClick={() => onCancelLabels(node)}
        >
          取消
        </Button>
      </div>
      {metadataError ? (
        <p className="nodes-table__inline-error" role="alert">
          {metadataError}
        </p>
      ) : null}
    </div>
  )
}
