import type { FormEvent } from 'react'

import { Button } from '../../components/atoms/Button'
import { TargetLabelsAndNote } from '../../components/target-detail'
import type { TargetRecord } from '../../lib/types'

type TargetMetadataSectionProps = {
  target: TargetRecord
  editing: boolean
  groupDraft: string
  labelDraft: string
  noteDraft: string
  submitting: boolean
  error: string | null
  canRebase: boolean
  onGroupDraftChange: (value: string) => void
  onLabelDraftChange: (value: string) => void
  onNoteDraftChange: (value: string) => void
  onStartEdit: () => void
  onCancelEdit: () => void
  onRebase: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function TargetMetadataSection({
  target,
  editing,
  groupDraft,
  labelDraft,
  noteDraft,
  submitting,
  error,
  canRebase,
  onGroupDraftChange,
  onLabelDraftChange,
  onNoteDraftChange,
  onStartEdit,
  onCancelEdit,
  onRebase,
  onSubmit,
}: TargetMetadataSectionProps) {
  return (
    <details className="watchtower-secondary">
      <summary>标签与备注</summary>
      <div className="watchtower-secondary__body">
        <TargetLabelsAndNote
          target={target}
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
          onSubmit={onSubmit}
        />
        {canRebase ? (
          <Button variant="ghost" size="sm" disabled={submitting} onClick={onRebase}>
            以当前版本为基准
          </Button>
        ) : null}
      </div>
    </details>
  )
}
