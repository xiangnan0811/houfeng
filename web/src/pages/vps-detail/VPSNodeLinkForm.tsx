import { useId, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import type { NodeRecord, VPSAssetDetail } from '../../lib/types'
import type { LinkDraftState } from './types'

type VPSNodeLinkFormProps = {
  detail: VPSAssetDetail
  draft: LinkDraftState
  nodes: NodeRecord[]
  nodesLoading: boolean
  nodesError: string | null
  controlsDisabled: boolean
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: LinkDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSNodeLinkForm({
  detail,
  draft,
  nodes,
  nodesLoading,
  nodesError,
  controlsDisabled,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSNodeLinkFormProps) {
  const nodeSelectId = useId()
  const noteId = useId()
  const linkedNodeIDs = new Set(detail.node_links.map((node) => node.node_id))
  const selectableNodes = nodes.filter((node) => !linkedNodeIDs.has(node.node_id))

  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <div>
          <h3>关联 Node</h3>
          <p>把资产台账中的 VPS 与观测系统中的 Node 对齐。</p>
        </div>
        <Badge variant="count" tone="neutral">{detail.node_links.length} 个 Node</Badge>
      </div>
      <label className="asset-operation-field" htmlFor={nodeSelectId}>
        <span>选择 Node</span>
        <select
          id={nodeSelectId}
          aria-label="选择 Node"
          value={draft.nodeId}
          disabled={controlsDisabled || nodesLoading || selectableNodes.length === 0}
          onChange={(event) => {
            onDraftChange({ ...draft, nodeId: event.target.value })
            onFeedbackClear()
          }}
        >
          <option value="">选择现有 Node</option>
          {selectableNodes.map((node) => (
            <option key={node.node_id} value={node.node_id}>
              {node.display_name} · {node.node_id} · {node.provider || 'provider 未填'} · {node.lifecycle_status} / {node.current_health_status}
            </option>
          ))}
        </select>
        <small>
          {nodesLoading
            ? '正在读取 Node 列表…'
            : nodesError
              ? `Node 列表不可用：${nodesError}`
              : selectableNodes.length === 0
                ? '没有可关联的 Node，请先创建并完成接入。'
                : '只创建资产↔Node 关联，不会修改 Node 生命周期、provider 或运行时状态。'}
          {' '}
          <Link className="text-link" to="/nodes">Node 列表</Link>
        </small>
      </label>
      <label className="asset-operation-field asset-operation-field--wide" htmlFor={noteId}>
        <span>关联备注</span>
        <textarea
          id={noteId}
          aria-label="关联备注"
          value={draft.note}
          onChange={(event) => {
            onDraftChange({ ...draft, note: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：主业务 Node"
          disabled={controlsDisabled}
        />
      </label>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <div className="asset-operation-actions">
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" disabled={controlsDisabled}>
          {submitting ? '关联中…' : '关联 Node'}
        </Button>
      </div>
    </form>
  )
}
