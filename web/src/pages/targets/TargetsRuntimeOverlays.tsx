import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import type { TargetRecord } from '../../lib/types'
import { pauseConfirmationCurrent } from './targetHelpers'
import type { PendingTargetConfirmation, TargetRuntimeAction } from './types'

type TargetsRuntimeOverlaysProps = {
  targets: TargetRecord[]
  pendingConfirmation: PendingTargetConfirmation | null
  runtimeErrors: Record<string, string>
  runtimeBusyTargetId: string | null
  onConfirmRuntimeAction: (
    target: TargetRecord,
    action: TargetRuntimeAction,
  ) => void
  onCancelConfirmation: (targetId: string, action: PendingTargetConfirmation['action']) => void
}

export function TargetsRuntimeOverlays({
  targets,
  pendingConfirmation,
  runtimeErrors,
  runtimeBusyTargetId,
  onConfirmRuntimeAction,
  onCancelConfirmation,
}: TargetsRuntimeOverlaysProps) {
  return (
    <>
      {targets.map((target) => {
        const runtimeError = runtimeErrors[target.target_id]
        const showConfirmation = pendingConfirmation?.targetId === target.target_id
        if (!runtimeError && !showConfirmation) return null
        return (
          <div key={`runtime-${target.target_id}`} className="targets-table__row-overlay">
            {showConfirmation ? (
              <ActionConfirmationModal
                open
                title={
                  pendingConfirmation.action === 'pause' ? '确认暂停目标监控' : '确认归档目标'
                }
                current={
                  pendingConfirmation.action === 'pause'
                    ? pauseConfirmationCurrent()
                    : '当前：目标仍在当前工作集中。'
                }
                result={
                  pendingConfirmation.action === 'pause'
                    ? '操作后：目标运行状态变为暂停。'
                    : '操作后：目标退出当前工作集，运行状态变为已归档。'
                }
                impact={
                  pendingConfirmation.action === 'pause'
                    ? '会停止该目标下所有 ProbeItem 的执行，不再产生新的入口探测记录。'
                    : '归档后不会继续作为活跃目标参与观测、异常判定或通知。'
                }
                unchanged={
                  pendingConfirmation.action === 'pause'
                    ? '不会删除历史事件、观测记录或 ProbeItem 配置。'
                    : '不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。'
                }
                confirmLabel={
                  pendingConfirmation.action === 'pause' ? '确认暂停目标' : '确认归档'
                }
                disabled={runtimeBusyTargetId === target.target_id}
                onConfirm={() => onConfirmRuntimeAction(target, pendingConfirmation.action)}
                onCancel={() => onCancelConfirmation(target.target_id, pendingConfirmation.action)}
              />
            ) : null}
            {runtimeError ? (
              <p className="targets-table__inline-error" role="alert" aria-live="assertive">
                {runtimeError}
              </p>
            ) : null}
          </div>
        )
      })}
    </>
  )
}
