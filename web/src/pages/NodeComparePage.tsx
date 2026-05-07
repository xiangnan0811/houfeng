import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { NodeWatchtowerMetrics } from '../components/node-detail'
import { Hostname, StatusGlyph } from '../components/atoms'
import { StatusBadge } from '../components/StatusBadge'
import { ApiError, getNode, getNodeRuntimeFacts } from '../lib/api'
import type { NodeRecord, NodeRuntimeFacts } from '../lib/types'

type NodeState = {
  loading: boolean
  error: string | null
  node: NodeRecord | null
  runtimeFacts: NodeRuntimeFacts | null
}

function useNodeData(nodeId: string | null): NodeState {
  const [state, setState] = useState<NodeState>(() => ({
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
        setState({ loading: false, error: null, node, runtimeFacts })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '节点不存在'
            : error instanceof Error
              ? error.message
              : '加载失败'
        setState({ loading: false, error: message, node: null, runtimeFacts: null })
      })

    return () => { cancelled = true }
  }, [nodeId])

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
      <section className="page-panel">
        <h2 className="page-panel__title">节点对比</h2>
        <p className="page-panel__description">
          请在节点列表页选中 2 个节点后进入对比视图。
        </p>
        <Link className="text-link" to="/nodes">返回节点列表</Link>
      </section>
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
  if (state.loading) {
    return <div className="compare-identity__card">正在加载…</div>
  }
  if (state.error || !state.node) {
    return <div className="compare-identity__card compare-identity__card--error">{state.error ?? '不可用'}</div>
  }
  const node = state.node
  return (
    <div className="compare-identity__card">
      <div className="compare-identity__header">
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
        />
        <Link className="text-link" to={`/nodes/${node.node_id}`}>
          {node.display_name}
        </Link>
        <span className="compare-identity__side">{side === 'left' ? 'A' : 'B'}</span>
      </div>
      <p className="compare-identity__meta">
        <Hostname truncate maxChars={24}>{node.node_id}</Hostname>
        {' · '}
        {[node.group, node.region, node.city, node.provider].filter(Boolean).join(' · ') || '未标记位置'}
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
  if (state.loading) return <div className="empty-state"><h3>正在加载…</h3></div>
  return <div className="empty-state"><h3>{state.error ?? '不可用'}</h3></div>
}
