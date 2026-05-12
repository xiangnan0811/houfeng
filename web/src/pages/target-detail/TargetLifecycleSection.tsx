import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import { Button } from '../../components/atoms/Button'
import type { TargetRuntimeAction } from '../../components/target-detail'

type TargetLifecycleSectionProps = {
  isArchived: boolean
  runtimeSubmitting: boolean
  probeConfirmationActive: boolean
  showArchiveConfirmation: boolean
  error: string | null
  onRestore: () => void
  onStartArchive: () => void
  onConfirmArchive: () => void
  onCancelArchive: () => void
  registerActionRef: (
    action: TargetRuntimeAction,
    element: HTMLButtonElement | null,
  ) => void
}

export function TargetLifecycleSection({
  isArchived,
  runtimeSubmitting,
  probeConfirmationActive,
  showArchiveConfirmation,
  error,
  onRestore,
  onStartArchive,
  onConfirmArchive,
  onCancelArchive,
  registerActionRef,
}: TargetLifecycleSectionProps) {
  return (
    <section className="watchtower-secondary">
      <div className="watchtower-secondary__body">
        <div className="watchtower-property-item">
          <div className="watchtower-property-item__main">
            <span className="watchtower-property-item__title">生命周期</span>
            <span className="watchtower-property-item__desc">
              {isArchived ? '目标处于已归档状态，可恢复至暂停以重新纳入工作集。' : '归档会退出当前工作集并保留历史。这不是删除，也不会清空事件、观测记录或 ProbeItem 配置。'}
            </span>
          </div>
          <div className="watchtower-property-item__actions">
            {isArchived ? (
              <Button
                variant="primary"
                ref={(element: HTMLButtonElement | null) => {
                  registerActionRef('restore-to-paused', element)
                }}
                disabled={runtimeSubmitting}
                onClick={onRestore}
              >
                {runtimeSubmitting ? '正在恢复…' : '恢复到暂停'}
              </Button>
            ) : (
              <Button
                variant="secondary"
                ref={(element: HTMLButtonElement | null) => {
                  registerActionRef('archive', element)
                }}
                disabled={runtimeSubmitting || probeConfirmationActive}
                onClick={onStartArchive}
              >
                归档
              </Button>
            )}
          </div>
        </div>
        {!isArchived && showArchiveConfirmation ? (
          <ActionConfirmationCard
            title="确认归档目标"
            current="当前：目标仍在当前工作集中。"
            result="操作后：目标退出当前工作集，运行状态变为已归档。"
            impact="归档后不会继续作为活跃目标参与观测、异常判定或通知。"
            unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。"
            confirmLabel="确认归档"
            error={error}
            disabled={runtimeSubmitting}
            onConfirm={onConfirmArchive}
            onCancel={onCancelArchive}
          />
        ) : null}
      </div>
    </section>
  )
}
