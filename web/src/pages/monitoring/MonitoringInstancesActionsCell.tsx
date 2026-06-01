import { Link } from 'react-router-dom'

import type { MonitoringInstanceRecord } from '../../lib/types'
import { actionButtonKey, monitoringInstanceRuntimeActions } from './monitoringHelpers'
import type { MonitoringInstanceRuntimeAction } from './types'

type MonitoringInstancesActionsCellProps = {
  monitoringInstance: MonitoringInstanceRecord
  editingLabelMonitoringInstanceId: string | null
  metadataBusyMonitoringInstanceId: string | null
  runtimeBusyMonitoringInstanceId: string | null
  actionButtonRefs: { current: Record<string, HTMLButtonElement | null> }
  onStartLabelEdit: (monitoringInstance: MonitoringInstanceRecord) => void
  onRuntimeAction: (monitoringInstance: MonitoringInstanceRecord, action: MonitoringInstanceRuntimeAction) => void
  onQueueFocusRestore: (monitoringInstanceId: string, action: MonitoringInstanceRuntimeAction) => void
}

export function MonitoringInstancesActionsCell({
  monitoringInstance,
  editingLabelMonitoringInstanceId,
  metadataBusyMonitoringInstanceId,
  runtimeBusyMonitoringInstanceId,
  actionButtonRefs,
  onStartLabelEdit,
  onRuntimeAction,
  onQueueFocusRestore,
}: MonitoringInstancesActionsCellProps) {
  const actions = monitoringInstanceRuntimeActions(monitoringInstance)
  return (
    <div
      className="monitoring-table__actions"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.stopPropagation()
        }
      }}
    >
      {editingLabelMonitoringInstanceId === monitoringInstance.monitoring_instance_id ? null : (
        <button
          type="button"
          className="btn sm secondary"
          disabled={metadataBusyMonitoringInstanceId !== null}
          onClick={() => onStartLabelEdit(monitoringInstance)}
        >
          快速编辑标签
        </button>
      )}
      <Link
        className="btn sm secondary"
        to={`/monitoring/${monitoringInstance.monitoring_instance_id}?onboarding=1`}
        onClick={(event) => event.stopPropagation()}
      >
        接入 agent
      </Link>
      <Link
        className="btn sm secondary"
        to={`/monitoring/${monitoringInstance.monitoring_instance_id}`}
        onClick={(event) => event.stopPropagation()}
      >
        详情
      </Link>
      {actions.map(({ action, label }) => (
        <button
          key={action}
          type="button"
          className="btn sm secondary"
          ref={(element) => {
            actionButtonRefs.current[actionButtonKey(monitoringInstance.monitoring_instance_id, action)] = element
          }}
          disabled={runtimeBusyMonitoringInstanceId === monitoringInstance.monitoring_instance_id}
          onClick={() => {
            if (action === 'pause') {
              onQueueFocusRestore(monitoringInstance.monitoring_instance_id, action)
            }
            onRuntimeAction(monitoringInstance, action)
          }}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
