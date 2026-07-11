import type { ReactNode } from 'react'

import { Button } from '../../components/atoms'
import type { TargetRecord } from '../../lib/types'

type TargetsLabelsCellProps = {
  target: TargetRecord
  editing: boolean
  metadataGroupInput: string
  metadataLabelInput: string
  metadataSavingTargetId: string | null
  metadataError: string | undefined
  onGroupInputChange: (value: string) => void
  onLabelInputChange: (value: string) => void
  onSaveMetadata: (target: TargetRecord) => void
  onCancelMetadata: (targetId: string) => void
}

function renderLabelsContent(target: TargetRecord): ReactNode {
  if (target.labels.length === 0) return '—'
  const visible = target.labels.slice(0, 3)
  const overflow = target.labels.length - visible.length
  if (overflow === 0) return visible.join(' · ')
  return (
    <>
      {visible.join(' · ')}
      <span className="targets-table__labels-more"> +{overflow}</span>
    </>
  )
}

export function TargetsLabelsCell({
  target,
  editing,
  metadataGroupInput,
  metadataLabelInput,
  metadataSavingTargetId,
  metadataError,
  onGroupInputChange,
  onLabelInputChange,
  onSaveMetadata,
  onCancelMetadata,
}: TargetsLabelsCellProps) {
  if (editing) {
    return (
      <>
        {/* a11y-allow-nonsemantic-click: event-propagation */}
        <div
          className="targets-table__label-editor"
          onClick={(event) => event.stopPropagation()}
          onKeyDown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.stopPropagation()
            }
          }}
        >
          <label className="targets-table__label-editor-field">
            <span className="visually-hidden">Group</span>
            <input
              name={`target-group-${target.target_id}`}
              value={metadataGroupInput}
              onChange={(event) => onGroupInputChange(event.target.value)}
              aria-label="Group"
              placeholder="Group"
            />
          </label>
          <label className="targets-table__label-editor-field">
            <span className="visually-hidden">标签</span>
            <input
              name={`target-labels-${target.target_id}`}
              value={metadataLabelInput}
              onChange={(event) => onLabelInputChange(event.target.value)}
              aria-label="标签"
            />
          </label>
          <div className="targets-table__label-editor-actions">
            <Button
              size="sm"
              variant="primary"
              disabled={metadataSavingTargetId === target.target_id}
              onClick={() => onSaveMetadata(target)}
            >
              {metadataSavingTargetId === target.target_id ? '正在保存…' : '保存标签'}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={metadataSavingTargetId === target.target_id}
              onClick={() => onCancelMetadata(target.target_id)}
            >
              取消
            </Button>
          </div>
          {metadataError ? (
            <p className="targets-table__inline-error" role="alert">
              {metadataError}
            </p>
          ) : null}
        </div>
      </>
    )
  }

  // Test fixture compatibility: existing tests assert on
  // "标签：alpha · beta" formatted text ("标签：" prefix + dotted list).
  // Keep the visible labels contiguous in a single text node so RTL's
  // exact-text matcher resolves it; the overflow "+N" suffix is in a
  // sibling span and never appears together with that exact assertion.
  return (
    <span className="targets-table__labels-cell">
      标签：{renderLabelsContent(target)}
    </span>
  )
}
