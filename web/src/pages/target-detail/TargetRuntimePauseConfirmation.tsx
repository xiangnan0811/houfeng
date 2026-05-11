import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import type { TargetRecord } from '../../lib/types'

type TargetRuntimePauseConfirmationProps = {
  target: TargetRecord
  disabled: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function TargetRuntimePauseConfirmation({
  target,
  disabled,
  onConfirm,
  onCancel,
}: TargetRuntimePauseConfirmationProps) {
  return (
    <ActionConfirmationCard
      title="确认暂停目标监控"
      current={
        target.run_status === '维护中'
          ? '当前：目标运行状态为维护中。'
          : '当前：目标运行状态为启用。'
      }
      result="操作后：目标运行状态变为暂停。"
      impact="会停止该目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。"
      unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。"
      confirmLabel="确认暂停目标"
      disabled={disabled}
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  )
}
