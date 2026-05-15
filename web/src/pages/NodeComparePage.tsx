import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { NodeWatchtowerMetrics } from '../components/node-detail'
import { Hostname, StatusGlyph } from '../components/atoms'
import { PageState } from '../components/PageState'
import { StatusBadge } from '../components/StatusBadge'
import { ApiError, getNode, getNodeRuntimeFacts } from '../lib/api'
import type { NodeRecord, NodeRuntimeFacts } from '../lib/types'

type NodeState = {
  loading: boolean
  error: string | null
  node: NodeRecord | null
  runtimeFacts: NodeRuntimeFacts | null
}

type StoredNodeState = NodeState & {
  nodeId: string | null
}

function useNodeData(nodeId: string | null): NodeState {
  const [state, setState] = useState<StoredNodeState>(() => ({
    nodeId,
    loading: !!nodeId,
    error: nodeId ? null : '缺少节点 ID',
    node: null,
    runtimeFacts: null,
  }))

  useEffect(() => {
    if (!nodeId) return
    let cancelled = false

    Promise.all([getNode(nodeId), getNodeRuntimeFacts(nodeId)])
      .then(([node, runtimeFacts]) => {
        if (cancelled) return
        setState({ nodeId, loading: false, error: null, node, runtimeFacts })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '节点不存在'
            : error instanceof Error
              ? error.message
              : '加载失败'
        setState({ nodeId, loading: false, error: message, node: null, runtimeFacts: null })
      })

    return () => { cancelled = true }
  }, [nodeId])

  if (!nodeId) {
    return { loading: false, error: '缺少节点 ID', node: null, runtimeFacts: null }
  }
  if (state.nodeId !== nodeId) {
    return { loading: true, error: null, node: null, runtimeFacts: null }
  }

  return state
}

export function NodeComparePage() {
  const [searchParams] = useSearchParams()
  const ids = searchParams.getAll('id')
  const idA = ids[0] ?? null
  const idB = ids[1] ?? null

  const nodeA = useNodeData(idA)
  const nodeB = useNodeData(idB)

  if (ids.length < 2) {
    return (
      <PageState
        kind="empty"
        eyebrow="节点对比"
        title="需要选择 2 个节点"
        description="请先在节点列表勾选两个节点，再进入 A / B 指标对比。"
        action={<Link className="btn btn--secondary btn--md" to="/nodes">返回节点列表</Link>}
      />
    )
  }

  return (
    <div className="page-stack">
      <header className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">节点对比</p>
          <h2 className="section-heading__title">指标对比</h2>
        </div>
        <Link className="btn btn--ghost btn--md" to="/nodes">返回节点列表</Link>
      </header>

      <div className="compare-identity">
        <CompareNodeIdentity state={nodeA} side="left" />
        <CompareNodeIdentity state={nodeB} side="right" />
      </div>

      <DetailSection title="主机指标对比">
        <div className="compare-metrics">
          <div className="compare-metrics__col">
            {!nodeA.loading && !nodeA.error && nodeA.runtimeFacts ? (
              <NodeWatchtowerMetrics
                sample={nodeA.runtimeFacts.latest_host_sample ?? null}
                samples={nodeA.runtimeFacts.recent_host_samples ?? []}
              />
            ) : (
              <CompareColumnPlaceholder state={nodeA} />
            )}
          </div>
          <div className="compare-metrics__col">
            {!nodeB.loading && !nodeB.error && nodeB.runtimeFacts ? (
              <NodeWatchtowerMetrics
                sample={nodeB.runtimeFacts.latest_host_sample ?? null}
                samples={nodeB.runtimeFacts.recent_host_samples ?? []}
              />
            ) : (
              <CompareColumnPlaceholder state={nodeB} />
            )}
          </div>
        </div>
      </DetailSection>
    </div>
  )
}

function CompareNodeIdentity({ state, side }: { state: NodeState; side: 'left' | 'right' }) {
  const label = side === 'left' ? 'A' : 'B'
  if (state.loading) {
    return (
      <PageState
        kind="loading"
        title={`${label} 节点读取中`}
        description="正在读取节点身份与最近运行事实，用于建立对比基线。"
        surface="empty"
        compact
        className="compare-identity__state"
      />
    )
  }
  if (state.error || !state.node) {
    return (
      <PageState
        kind="error"
        title={`${label} 节点不可用`}
        description="当前节点无法参与对比，请返回节点列表重新选择。"
        technicalSummary={state.error ?? '节点不可用'}
        surface="empty"
        compact
        className="compare-identity__state"
      />
    )
  }
  const node = state.node
  const location = [node.group, node.region, node.city, node.provider].filter(Boolean).join(' · ') || '未标记位置'
  return (
    <div className="compare-identity__card">
      <div className="compare-identity__header">
        <span className="compare-identity__side">{label}</span>
        <StatusGlyph
          state={
            node.monitoring_status === '维护中' ? 'maintenance'
            : node.monitoring_status === '暂停' ? 'offline'
            : node.current_health_status === '正常' ? 'normal'
            : node.current_health_status === '关注' ? 'notice'
            : node.current_health_status === '告警' ? 'alert'
            : node.current_health_status === '严重' ? 'critical'
            : 'offline'
          }
          size="md"
          ariaLabel={`${label} 节点健康状态`}
        />
        <div className="compare-identity__title">
          <span>对比对象 {label}</span>
          <Link className="text-link" to={`/nodes/${node.node_id}`}>
            {node.display_name}
          </Link>
        </div>
        <Link className="compare-identity__detail" to={`/nodes/${node.node_id}`}>
          节点详情
        </Link>
      </div>
      <p className="compare-identity__meta">
        <Hostname truncate maxChars={24}>{node.node_id}</Hostname>
        <span>{location}</span>
      </p>
      <div className="badge-row">
        <StatusBadge label={node.lifecycle_status} />
        <StatusBadge label={node.monitoring_status} />
        <StatusBadge label={node.binding_status} />
        <StatusBadge label={node.current_health_status} />
      </div>
    </div>
  )
}

function CompareColumnPlaceholder({ state }: { state: NodeState }) {
  if (state.loading) {
    return (
      <PageState
        kind="loading"
        title="指标读取中"
        description="正在读取最近主机样本，图表会在运行事实可用后显示。"
        surface="empty"
        compact
      />
    )
  }
  return (
    <PageState
      kind="error"
      title="指标不可用"
      description="当前节点没有可用于对比的主机指标。"
      technicalSummary={state.error ?? '指标不可用'}
      surface="empty"
      compact
      action={<Link className="btn btn--ghost btn--sm" to="/nodes">返回节点列表重新选择</Link>}
    />
  )
}
