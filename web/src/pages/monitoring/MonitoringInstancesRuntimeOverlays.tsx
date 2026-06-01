import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { pauseConfirmationCurrent } from './monitoringHelpers'
import type { PendingMonitoringInstanceConfirmation } from './types'

type MonitoringInstancesRuntimeOverlaysProps = {
  monitoring: MonitoringInstanceRecord[]
  runtimeErrors: Record<string, string>
  pendingConfirmation: PendingMonitoringInstanceConfirmation | null
  runtimeBusyMonitoringInstanceId: string | null
  onConfirmPause: (monitoringInstance: MonitoringInstanceRecord) => void
  onCancelPause: (monitoringInstance: MonitoringInstanceRecord) => void
}

export function MonitoringInstancesRuntimeOverlays({
  monitoring,
  runtimeErrors,
  pendingConfirmation,
  runtimeBusyMonitoringInstanceId,
  onConfirmPause,
  onCancelPause,
}: MonitoringInstancesRuntimeOverlaysProps) {
  return (
    <>
      {monitoring.map((monitoringInstance) => {
        const runtimeError = runtimeErrors[monitoringInstance.monitoring_instance_id]
        const showPauseConfirmation =
          pendingConfirmation?.monitoringInstanceId === monitoringInstance.monitoring_instance_id &&
          pendingConfirmation.action === 'pause'
        if (!runtimeError && !showPauseConfirmation) return null
        return (
          <div key={`runtime-${monitoringInstance.monitoring_instance_id}`} className="monitoring-table__row-overlay">
            {showPauseConfirmation ? (
              <ActionConfirmationCard
                title="确认暂停监控实例监控"
                current={pauseConfirmationCurrent(monitoringInstance)}
                result="操作后：监控运行状态变为暂停。"
                impact="会停止主机指标采集，并停止该监控实例承担的探针执行。趋势图会从此开始出现数据空档。"
                unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
                confirmLabel="确认暂停监控"
                disabled={runtimeBusyMonitoringInstanceId === monitoringInstance.monitoring_instance_id}
                onConfirm={() => onConfirmPause(monitoringInstance)}
                onCancel={() => onCancelPause(monitoringInstance)}
              />
            ) : null}
            {runtimeError ? (
              <p className="monitoring-table__inline-error" role="alert">
                {monitoringInstance.display_name}：{runtimeError}
              </p>
            ) : null}
          </div>
        )
      })}
    </>
  )
}
