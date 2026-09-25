import { useState } from 'react'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import type { GlobalActionConfirmation, TargetRecord } from '../../lib/types'
import type { TargetRuntimeAction } from '../../components/target-detail'
import { TargetSharedImpactFields } from './TargetSharedImpactFields'

type TargetRuntimePauseConfirmationProps = {
  target: TargetRecord
  action?: TargetRuntimeAction
  disabled: boolean
  error?: string | null
  reviewGeneration?: number
  onConfirm: (confirmation?: GlobalActionConfirmation) => void
  onCancel: () => void
}

function actionModalContent(action: TargetRuntimeAction, target: TargetRecord) {
  switch (action) {
    case 'resume':
      return {
        title: '确认恢复目标监控',
        current: `当前：目标运行状态为${target.run_status}。`,
        result: '操作后：目标运行状态变为启用。',
        impact: '会恢复该目标下已启用 ProbeItem 的探测执行。',
        unchanged: '不会修改历史事件或既有探测配置。',
        confirmLabel: '确认恢复目标',
      }
    case 'enter-maintenance':
      return {
        title: '确认进入维护模式',
        current: `当前：目标运行状态为${target.run_status}。`,
        result: '操作后：目标运行状态变为维护中。',
        impact: '观测可以继续，异常判定与告警通知将被抑制。',
        unchanged: '不会删除历史事件、观测记录或既有配置。',
        confirmLabel: '确认进入维护',
      }
    case 'exit-maintenance':
      return {
        title: '确认退出维护模式',
        current: '当前：目标运行状态为维护中。',
        result: '操作后：目标运行状态恢复为启用。',
        impact: '恢复正常的探测异常判定与告警通知。',
        unchanged: '不会修改历史事件或既有探测配置。',
        confirmLabel: '确认退出维护',
      }
    case 'restore-to-paused':
      return {
        title: '确认恢复已归档目标',
        current: '当前：目标处于已归档状态。',
        result: '操作后：目标运行状态变为暂停，重新纳入工作集。',
        impact: '恢复至暂停状态，便于在重新启用前检查配置与关联关系。',
        unchanged: '不会删除历史事件、观测记录或 ProbeItem 配置。',
        confirmLabel: '确认恢复到暂停',
      }
    case 'archive':
      return {
        title: '确认归档目标',
        current: '当前：目标仍在当前工作集中。',
        result: '操作后：目标退出当前工作集，运行状态变为已归档。',
        impact: '归档后不会继续作为活跃目标参与观测、异常判定或通知。',
        unchanged: '不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。',
        confirmLabel: '确认归档',
      }
    case 'pause':
    default:
      return {
        title: '确认暂停目标监控',
        current:
          target.run_status === '维护中'
            ? '当前：目标运行状态为维护中。'
            : '当前：目标运行状态为启用。',
        result: '操作后：目标运行状态变为暂停。',
        impact: '会停止该目标下所有 ProbeItem 的执行，不再产生新的入口探测记录。',
        unchanged: '不会删除历史事件、观测记录或 ProbeItem 配置。',
        confirmLabel: '确认暂停目标',
      }
  }
}

export function TargetRuntimePauseConfirmation({
  target,
  action = 'pause',
  disabled,
  error = null,
  reviewGeneration = 0,
  onConfirm,
  onCancel,
}: TargetRuntimePauseConfirmationProps) {
  const [blocked, setBlocked] = useState(true)
  const [confirmation, setConfirmation] = useState<GlobalActionConfirmation | undefined>(undefined)
  const [seenGeneration, setSeenGeneration] = useState(reviewGeneration)
  if (seenGeneration !== reviewGeneration) {
    setSeenGeneration(reviewGeneration)
    setBlocked(true)
    setConfirmation(undefined)
  }

  const content = actionModalContent(action, target)
  return (
    <ActionConfirmationModal
      open
      title={content.title}
      current={content.current}
      result={content.result}
      impact={content.impact}
      unchanged={content.unchanged}
      confirmLabel={content.confirmLabel}
      disabled={disabled || blocked}
      cancelDisabled={disabled}
      error={error}
      onConfirm={() => {
        if (blocked) return
        onConfirm(confirmation)
      }}
      onCancel={onCancel}
    >
      <TargetSharedImpactFields
        targetId={target.target_id}
        reviewGeneration={reviewGeneration}
        onChange={(next, nextBlocked) => {
          setConfirmation(next)
          setBlocked(nextBlocked)
        }}
      />
    </ActionConfirmationModal>
  )
}
