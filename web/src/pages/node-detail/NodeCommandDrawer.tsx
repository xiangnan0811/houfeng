import { Drawer } from '../../components/atoms'
import type { NodeRecord } from '../../lib/types'
import type { NodeCommand } from './types'
import { NodeCommandResult } from './NodeCommandResult'

type NodeCommandDrawerProps = {
  node: NodeRecord
  open: boolean
  commands: NodeCommand[]
  commandLabels: Record<string, string>
  submitting: boolean
  error: string | null
  onClose: () => void
  onExecute: (commandId: string) => void
}

export function NodeCommandDrawer({
  node,
  open,
  commands,
  commandLabels,
  submitting,
  error,
  onClose,
  onExecute,
}: NodeCommandDrawerProps) {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title="执行命令"
      ariaLabel="执行命令抽屉"
    >
      <div className="command-picker">
        {commands.map((command) => (
          <button
            key={command.id}
            className="btn md secondary command-picker__item"
            disabled={submitting || node.last_action?.status === 'pending'}
            onClick={() => onExecute(command.id)}
          >
            <span className="command-picker__name">{command.name}</span>
            <span className="command-picker__desc">{command.description}</span>
          </button>
        ))}
      </div>

      {error ? (
        <p className="watchtower-runtime-error" role="alert">
          {error}
        </p>
      ) : null}

      {node.last_action ? (
        <NodeCommandResult action={node.last_action} labels={commandLabels} />
      ) : null}
    </Drawer>
  )
}
