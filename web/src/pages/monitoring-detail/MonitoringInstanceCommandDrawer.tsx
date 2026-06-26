import { useState } from 'react'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
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
  onExecute: (commandId: string, options?: { confirmedSensitive?: boolean }) => void
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
  const [pendingSensitiveCommand, setPendingSensitiveCommand] = useState<MonitoringInstanceCommand | null>(null)
  const commandPending = submitting || monitoringInstance.last_action?.status === 'pending'

  function handleCommandClick(command: MonitoringInstanceCommand) {
    if (command.sensitivity === 'sensitive') {
      setPendingSensitiveCommand(command)
      return
    }
    onExecute(command.id)
  }

  function handleConfirmSensitiveCommand() {
    if (!pendingSensitiveCommand) return
    const commandID = pendingSensitiveCommand.id
    setPendingSensitiveCommand(null)
    onExecute(commandID, { confirmedSensitive: true })
  }

  return (
    <>
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
              disabled={commandPending}
              onClick={() => handleCommandClick(command)}
            >
              <span className="command-picker__name">
                {command.name}
                {command.sensitivity === 'sensitive' ? (
                  <span className="command-picker__sensitivity">敏感</span>
                ) : null}
              </span>
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

      <ActionConfirmationModal
        open={pendingSensitiveCommand != null}
        title="确认执行敏感命令"
        current={pendingSensitiveCommand?.name ?? ''}
        result="等待 agent 执行"
        impact="该命令的输出可能包含服务、进程、日志或容器等运行细节。"
        unchanged="命令仍由 agent 编译期白名单执行，不接受自定义参数。"
        confirmLabel="确认执行"
        disabled={submitting}
        onCancel={() => setPendingSensitiveCommand(null)}
        onConfirm={handleConfirmSensitiveCommand}
      />
    </>
  )
}
