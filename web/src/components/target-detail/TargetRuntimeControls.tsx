import { ActionConfirmationModal } from '../ActionConfirmationModal'
import { DetailSection } from '../DetailSection'
import type { TargetRecord } from '../../lib/types'

export type TargetRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'
  | 'archive'
  | 'restore-to-paused'

export type PendingRuntimeConfirmation = {
  action: 'pause' | 'archive'
}

const RUNTIME_ACTION_BUTTONS_BY_RUN_STATUS: Record<
  string,
  Array<{ action: TargetRuntimeAction; label: string }>
> = {
  启用: [
    { action: 'enter-maintenance', label: '进入维护' },
    { action: 'pause', label: '暂停' },
    { action: 'archive', label: '退役' },
  ],
  维护中: [
    { action: 'exit-maintenance', label: '退出维护' },
    { action: 'pause', label: '暂停' },
    { action: 'archive', label: '退役' },
  ],
  暂停: [
    { action: 'resume', label: '恢复' },
    { action: 'archive', label: '退役' },
  ],
}

function targetRuntimeActions(
  target: TargetRecord,
): Array<{ action: TargetRuntimeAction; label: string }> {
  if (target.lifecycle_status === 'retired') return [{ action: 'restore-to-paused', label: '恢复到暂停' }]
  return RUNTIME_ACTION_BUTTONS_BY_RUN_STATUS[target.run_status] ?? []
}

type TargetRuntimeControlsProps = {
  target: TargetRecord
  disabled: boolean
  submitting: boolean
  error: string | null
  pendingConfirmation: PendingRuntimeConfirmation | null
  onAction: (action: TargetRuntimeAction) => void
  onConfirm: (action: PendingRuntimeConfirmation['action']) => void
  onCancelConfirmation: (action: PendingRuntimeConfirmation['action']) => void
  registerActionButtonRef: (
    action: TargetRuntimeAction,
    element: HTMLButtonElement | null,
  ) => void
}

export function TargetRuntimeControls({
  target,
  disabled,
  submitting,
  error,
  pendingConfirmation,
  onAction,
  onConfirm,
  onCancelConfirmation,
  registerActionButtonRef,
}: TargetRuntimeControlsProps) {
  return (
    <DetailSection eyebrow="运行控制" title="运行控制">
      <div className="page-stack">
        <p>
          维护会继续采集，但不解释结果。暂停会停止采集并产生数据空档。退役会退出当前工作集并保留历史。
        </p>
        <div className="badge-row badge-row--wrap">
          {targetRuntimeActions(target).map(({ action, label }) => (
            <button
              key={action}
              ref={(element) => {
                registerActionButtonRef(action, element)
              }}
              type="button"
              disabled={disabled}
              onClick={() => onAction(action)}
            >
              {label}
            </button>
          ))}
        </div>
        {pendingConfirmation ? (
          <ActionConfirmationModal
            open
            title={
              pendingConfirmation.action === 'pause'
                ? '确认暂停目标监控'
                : '确认退役目标'
            }
            current={
              pendingConfirmation.action === 'pause'
                ? '当前：目标运行状态为启用或维护中。'
                : '当前：目标仍在当前工作集中。'
            }
            result={
              pendingConfirmation.action === 'pause'
                ? '操作后：目标运行状态变为暂停。'
                : '操作后：目标退出当前工作集，生命周期变为已退役，运行控制保持暂停。'
            }
            impact={
              pendingConfirmation.action === 'pause'
                ? '会停止该目标下所有探测项的执行，不再产生新的入口探测记录。'
                : '退役后不会继续作为活跃目标参与观测、异常判定或通知。'
            }
            unchanged={
              pendingConfirmation.action === 'pause'
                ? '不会删除历史事件、观测记录或探测项配置。'
                : '不会删除历史事件、观测记录或探测项配置。后续可恢复到暂停。'
            }
            confirmLabel={
              pendingConfirmation.action === 'pause' ? '确认暂停目标' : '确认退役'
            }
            disabled={submitting}
            onConfirm={() => onConfirm(pendingConfirmation.action)}
            onCancel={() => onCancelConfirmation(pendingConfirmation.action)}
          />
        ) : null}
        {error ? <p>{error}</p> : null}
      </div>
    </DetailSection>
  )
}
