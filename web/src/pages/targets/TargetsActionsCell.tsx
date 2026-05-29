import { Link } from 'react-router-dom'

import type { TargetRecord } from '../../lib/types'
import {
  actionButtonKey,
  targetRuntimeActions,
} from './targetHelpers'
import type { TargetRuntimeAction } from './types'

type TargetsActionsCellProps = {
  target: TargetRecord
  metadataEditingTargetId: string | null
  metadataSavingTargetId: string | null
  runtimeBusyTargetId: string | null
  actionButtonRefs: { current: Record<string, HTMLButtonElement | null> }
  onStartMetadataEdit: (target: TargetRecord) => void
  onRuntimeAction: (target: TargetRecord, action: TargetRuntimeAction) => void
}

export function TargetsActionsCell({
  target,
  metadataEditingTargetId,
  metadataSavingTargetId,
  runtimeBusyTargetId,
  actionButtonRefs,
  onStartMetadataEdit,
  onRuntimeAction,
}: TargetsActionsCellProps) {
  const actions = targetRuntimeActions(target)

  return (
    <div
      className="targets-table__actions"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.stopPropagation()
        }
      }}
    >
      {metadataEditingTargetId === target.target_id ? null : (
        <button
          type="button"
          className="btn sm secondary"
          disabled={metadataSavingTargetId !== null}
          onClick={() => onStartMetadataEdit(target)}
        >
          快速编辑标签
        </button>
      )}
      <Link
        className="btn-text sm primary"
        to={`/targets/${target.target_id}`}
        onClick={(event) => event.stopPropagation()}
      >
        详情
      </Link>
      {actions.map(({ action, label }) => (
        <button
          key={action}
          ref={(element) => {
            actionButtonRefs.current[actionButtonKey(target.target_id, action)] = element
          }}
          type="button"
          className={`btn sm ${action === 'archive' ? 'danger' : 'secondary'}`}
          disabled={runtimeBusyTargetId === target.target_id}
          onClick={() => onRuntimeAction(target, action)}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
