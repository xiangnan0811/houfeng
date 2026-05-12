import { NodeLabelsAndNote } from '../../components/node-detail'
import type { NodeRecord } from '../../lib/types'

type NodeMetadataSectionProps = {
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

export function NodeMetadataSection({
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
}: NodeMetadataSectionProps) {
  return (
    <NodeLabelsAndNote
      node={node}
      editing={editing}
      groupDraft={groupDraft}
      labelDraft={labelDraft}
      noteDraft={noteDraft}
      submitting={submitting}
      error={error}
      onGroupDraftChange={onGroupDraftChange}
      onLabelDraftChange={onLabelDraftChange}
      onNoteDraftChange={onNoteDraftChange}
      onStartEdit={onStartEdit}
      onCancelEdit={onCancelEdit}
      onSave={onSave}
    />
  )
}
