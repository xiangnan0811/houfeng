import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'
import type { NodeRecord } from '../../lib/types'
import { pauseConfirmationCurrent } from './nodeHelpers'
import type { PendingNodeConfirmation } from './types'

type NodesRuntimeOverlaysProps = {
  nodes: NodeRecord[]
  runtimeErrors: Record<string, string>
  pendingConfirmation: PendingNodeConfirmation | null
  runtimeBusyNodeId: string | null
  onConfirmPause: (node: NodeRecord) => void
  onCancelPause: (node: NodeRecord) => void
}

export function NodesRuntimeOverlays({
  nodes,
  runtimeErrors,
  pendingConfirmation,
  runtimeBusyNodeId,
  onConfirmPause,
  onCancelPause,
}: NodesRuntimeOverlaysProps) {
  return (
    <>
      {nodes.map((node) => {
        const runtimeError = runtimeErrors[node.node_id]
        const showPauseConfirmation =
          pendingConfirmation?.nodeId === node.node_id &&
          pendingConfirmation.action === 'pause'
        if (!runtimeError && !showPauseConfirmation) return null
        return (
          <div key={`runtime-${node.node_id}`} className="nodes-table__row-overlay">
            {showPauseConfirmation ? (
              <ActionConfirmationCard
                title="确认暂停节点监控"
                current={pauseConfirmationCurrent(node)}
                result="操作后：监控运行状态变为暂停。"
                impact="会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。"
                unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
                confirmLabel="确认暂停监控"
                disabled={runtimeBusyNodeId === node.node_id}
                onConfirm={() => onConfirmPause(node)}
                onCancel={() => onCancelPause(node)}
              />
            ) : null}
            {runtimeError ? (
              <p className="nodes-table__inline-error" role="alert">
                {node.display_name}：{runtimeError}
              </p>
            ) : null}
          </div>
        )
      })}
    </>
  )
}
