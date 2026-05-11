import { Link } from 'react-router-dom'

import { Button } from '../../components/atoms'
import type { NodeRecord } from '../../lib/types'
import { actionButtonKey, nodeRuntimeActions } from './nodeHelpers'
import type { NodeRuntimeAction } from './types'

type NodesActionsCellProps = {
  node: NodeRecord
  editingLabelNodeId: string | null
  metadataBusyNodeId: string | null
  runtimeBusyNodeId: string | null
  actionButtonRefs: { current: Record<string, HTMLButtonElement | null> }
  onStartLabelEdit: (node: NodeRecord) => void
  onRuntimeAction: (node: NodeRecord, action: NodeRuntimeAction) => void
  onQueueFocusRestore: (nodeId: string, action: NodeRuntimeAction) => void
}

export function NodesActionsCell({
  node,
  editingLabelNodeId,
  metadataBusyNodeId,
  runtimeBusyNodeId,
  actionButtonRefs,
  onStartLabelEdit,
  onRuntimeAction,
  onQueueFocusRestore,
}: NodesActionsCellProps) {
  const actions = nodeRuntimeActions(node)
  return (
    <div
      className="nodes-table__actions"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.stopPropagation()
        }
      }}
    >
      {editingLabelNodeId === node.node_id ? null : (
        <Button
          size="sm"
          variant="ghost"
          disabled={metadataBusyNodeId !== null}
          onClick={() => onStartLabelEdit(node)}
        >
          快速编辑标签
        </Button>
      )}
      <Link
        className="btn btn--ghost btn--sm"
        to={`/nodes/${node.node_id}/onboarding`}
        onClick={(event) => event.stopPropagation()}
      >
        接入工作台
      </Link>
      {actions.map(({ action, label }) => (
        <button
          key={action}
          type="button"
          className="btn btn--ghost btn--sm"
          ref={(element) => {
            actionButtonRefs.current[actionButtonKey(node.node_id, action)] = element
          }}
          disabled={runtimeBusyNodeId === node.node_id}
          onClick={() => {
            if (action === 'pause') {
              onQueueFocusRestore(node.node_id, action)
            }
            onRuntimeAction(node, action)
          }}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
