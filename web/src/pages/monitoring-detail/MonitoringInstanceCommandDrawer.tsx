import { Modal } from '../../components/atoms'
import type { MonitoringInstanceRecord } from '../../lib/types'
import type { MonitoringInstanceCommand } from './types'
import { MonitoringInstanceCommandResult } from './MonitoringInstanceCommandResult'

type MonitoringInstanceCommandDrawerProps = {
  monitoringInstance: MonitoringInstanceRecord
  open: boolean
  commands: MonitoringInstanceCommand[]
  commandLabels: Record<string, string>
  submitting: boolean
  error: string | null
  onClose: () => void
  onExecute: (commandId: string) => void
}

export function MonitoringInstanceCommandDrawer({
  monitoringInstance,
  open,
  commands,
  commandLabels,
  submitting,
  error,
  onClose,
  onExecute,
}: MonitoringInstanceCommandDrawerProps) {
  return (
    <Modal
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
            disabled={submitting || monitoringInstance.last_action?.status === 'pending'}
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

      {monitoringInstance.last_action ? (
        <MonitoringInstanceCommandResult action={monitoringInstance.last_action} labels={commandLabels} />
      ) : null}
    </Modal>
  )
}
