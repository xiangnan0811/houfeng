import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { pauseConfirmationCurrent } from './monitoringDetailHelpers'

type MonitoringInstanceRuntimePauseConfirmationProps = {
  monitoringInstance: MonitoringInstanceRecord
  disabled: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function MonitoringInstanceRuntimePauseConfirmation({
  monitoringInstance,
  disabled,
  onConfirm,
  onCancel,
}: MonitoringInstanceRuntimePauseConfirmationProps) {
  return (
    <ActionConfirmationCard
      title="确认暂停监控实例监控"
      current={pauseConfirmationCurrent(monitoringInstance)}
      result="操作后：监控运行状态变为暂停。"
      impact="会停止主机指标采集，并停止该监控实例承担的探针执行。趋势图会从此开始出现数据空档。"
      unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
      confirmLabel="确认暂停监控"
      disabled={disabled}
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  )
}
