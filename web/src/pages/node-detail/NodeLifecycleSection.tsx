import { CollapsibleSection } from '../../components/CollapsibleSection'
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
  return (
    <CollapsibleSection title="生命周期" className="watchtower-secondary">
      <div className="page-stack">
        {isRetiredNode ? <p>{NODE_LIFECYCLE_V1_LIMITATION_COPY}</p> : null}
        <div className="badge-row badge-row--wrap">
          {isRetiredNode ? (
            <button
              type="button"
              disabled={submitting !== null}
              onClick={onRestore}
            >
              {submitting === 'restore-to-observing' ? '正在恢复…' : '恢复到观察中'}
            </button>
          ) : (
            <button
              type="button"
              disabled={submitting !== null}
              onClick={onStartRetire}
            >
              退役节点
            </button>
          )}
        </div>
        {!isRetiredNode && showRetireConfirmation ? (
          <div className="page-stack">
            <p>退役会让节点退出当前工作集，但会保留历史记录。这不是删除，也不会清空事件、观测记录或 agent 绑定历史。</p>
            <div className="badge-row badge-row--wrap">
              <button
                type="button"
                disabled={submitting !== null}
                onClick={onConfirmRetire}
              >
                {submitting === 'retire' ? '正在退役…' : '确认退役'}
              </button>
              <button
                type="button"
                disabled={submitting !== null}
                onClick={onCancelRetire}
              >
                取消
              </button>
            </div>
          </div>
        ) : null}
        {error ? <p role="alert">{error}</p> : null}
      </div>
    </CollapsibleSection>
  )
}
