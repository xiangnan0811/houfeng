import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import { Button } from '../../components/atoms/Button'
import {
  NODE_LIFECYCLE_V1_LIMITATION_COPY,
} from './nodeDetailConstants'
import type { NodeLifecycleAction } from './types'

type NodeLifecycleSectionProps = {
  isRetiredNode: boolean
  showRetireConfirmation: boolean
  submitting: NodeLifecycleAction | null
  error: string | null
  onRestore: () => void
  onStartRetire: () => void
  onConfirmRetire: () => void
  onCancelRetire: () => void
}

export function NodeLifecycleSection({
  isRetiredNode,
  showRetireConfirmation,
  submitting,
  error,
  onRestore,
  onStartRetire,
  onConfirmRetire,
  onCancelRetire,
}: NodeLifecycleSectionProps) {
  const stacked = showRetireConfirmation || Boolean(error)

  return (
    <div className={`watchtower-property-item${stacked ? ' watchtower-property-item--stacked' : ''}`}>
      <div className="watchtower-property-item__main">
        <span className="watchtower-property-item__title">生命周期操作</span>
        <span className="watchtower-property-item__desc">
          {isRetiredNode ? NODE_LIFECYCLE_V1_LIMITATION_COPY : '退役会让节点退出当前工作集，但保留历史观测记录。'}
        </span>
      </div>
      <div className="watchtower-property-item__actions">
        {isRetiredNode ? (
          <Button
            variant="primary"
            disabled={submitting !== null}
            onClick={onRestore}
          >
            {submitting === 'restore-to-observing' ? '正在恢复…' : '恢复到观察中'}
          </Button>
        ) : showRetireConfirmation ? null : (
          <Button
            variant="secondary"
            disabled={submitting !== null}
            onClick={onStartRetire}
          >
            退役节点
          </Button>
        )}
      </div>
      {!isRetiredNode && showRetireConfirmation ? (
        <ActionConfirmationCard
          title="确认退役节点"
          current="当前：节点仍在当前工作集中。"
          result="操作后：节点生命周期变为已退役。"
          impact="这不是删除，会让节点退出当前工作集并停止承担观测任务。"
          unchanged="不会清空事件、观测记录或 agent 绑定历史。"
          confirmLabel={submitting === 'retire' ? '正在退役…' : '确认退役'}
          error={error}
          disabled={submitting !== null}
          onConfirm={onConfirmRetire}
          onCancel={onCancelRetire}
        />
      ) : error ? (
        <p className="watchtower-runtime-error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}
