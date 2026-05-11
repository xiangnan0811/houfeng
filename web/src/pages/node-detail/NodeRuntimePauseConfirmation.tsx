import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import type { NodeRecord } from '../../lib/types'
import { pauseConfirmationCurrent } from './nodeDetailHelpers'

type NodeRuntimePauseConfirmationProps = {
  node: NodeRecord
  disabled: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function NodeRuntimePauseConfirmation({
  node,
  disabled,
  onConfirm,
  onCancel,
}: NodeRuntimePauseConfirmationProps) {
  return (
    <ActionConfirmationCard
      title="确认暂停节点监控"
      current={pauseConfirmationCurrent(node)}
      result="操作后：监控运行状态变为暂停。"
      impact="会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。"
      unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
      confirmLabel="确认暂停监控"
      disabled={disabled}
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  )
}
