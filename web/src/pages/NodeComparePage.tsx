import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { NodeWatchtowerMetrics } from '../components/node-detail'
import { Hostname, MonoDigits, StatusGlyph, Timestamp, type HealthState } from '../components/atoms'
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

type CompareSide = 'left' | 'right'

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
        action={<Link className="btn md secondary" to="/nodes">返回节点列表</Link>}
      />
    )
  }

  return (
    <div className="page-stack">
      <CompareCommandPanel stateA={nodeA} stateB={nodeB} />

      <div className="compare-identity">
        <CompareNodeIdentity state={nodeA} side="left" />
        <CompareNodeIdentity state={nodeB} side="right" />
      </div>

      <CompareSummaryStrip stateA={nodeA} stateB={nodeB} />

      <DetailSection
        eyebrow="24h runtime facts"
        title="主机指标对比"
        aside="详细趋势仍使用 NodeWatchtowerMetrics"
      >
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

function sideLabel(side: CompareSide): 'A' | 'B' {
  return side === 'left' ? 'A' : 'B'
}

function nodeHealthGlyphState(node: NodeRecord): HealthState {
  if (node.monitoring_status === '维护中') return 'maintenance'
  if (node.monitoring_status === '暂停') return 'offline'
  if (node.current_health_status === '正常') return 'normal'
  if (node.current_health_status === '关注') return 'notice'
  if (node.current_health_status === '告警') return 'alert'
  if (node.current_health_status === '严重') return 'critical'
  return 'offline'
}

function nodeContext(node: NodeRecord): string {
  const parts = [node.group, node.provider, node.region, node.city].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '位置上下文未标记'
}

function CompareCommandPanel({ stateA, stateB }: { stateA: NodeState; stateB: NodeState }) {
  return (
    <section className="compare-command" aria-labelledby="node-compare-title">
      <div className="compare-command__intro">
        <p className="compare-command__eyebrow">节点对比 · 24h runtime facts</p>
        <h1 id="node-compare-title">判断两个 Node 是否需要深入排查</h1>
        <p>
          先对齐 A/B 的身份、健康、运行态、绑定态、位置与样本可用性；只有差异明显时再下钻详细主机指标。
        </p>
      </div>
      <div className="compare-command__aside">
        <div className="compare-command__selection" aria-label="当前对比对象">
          <CompareCommandPeer state={stateA} side="left" />
          <CompareCommandPeer state={stateB} side="right" />
        </div>
        <Link className="btn md ghost" to="/nodes">返回节点列表</Link>
      </div>
    </section>
  )
}

function CompareCommandPeer({ state, side }: { state: NodeState; side: CompareSide }) {
  const label = sideLabel(side)
  if (state.loading) {
    return (
      <article className="compare-command-peer">
        <span className="compare-command-peer__side">{label}</span>
        <div className="compare-command-peer__body">
          <span className="compare-command-peer__label">{label} 节点</span>
          <strong>读取中</strong>
          <span>正在读取身份与运行事实</span>
        </div>
      </article>
    )
  }
  if (state.error || !state.node) {
    return (
      <article className="compare-command-peer compare-command-peer--error">
        <span className="compare-command-peer__side">{label}</span>
        <div className="compare-command-peer__body">
          <span className="compare-command-peer__label">{label} 节点</span>
          <strong>不可用</strong>
          <span>{state.error ?? '节点不可用'}</span>
        </div>
      </article>
    )
  }
  const node = state.node
  return (
    <article className="compare-command-peer">
      <span className="compare-command-peer__side">{label}</span>
      <StatusGlyph state={nodeHealthGlyphState(node)} size="sm" ariaLabel={`${label} 节点健康状态`} />
      <div className="compare-command-peer__body">
        <span className="compare-command-peer__label">{label} 节点</span>
        <strong>{node.display_name}</strong>
        <span>{node.current_health_status} · {node.monitoring_status}</span>
      </div>
    </article>
  )
}

function CompareSummaryStrip({ stateA, stateB }: { stateA: NodeState; stateB: NodeState }) {
  return (
    <section className="compare-summary-strip" aria-labelledby="compare-summary-title">
      <header className="compare-summary-strip__header">
        <div>
          <p className="compare-summary-strip__eyebrow">Compare Summary</p>
          <h2 id="compare-summary-title">A/B 摘要判断</h2>
        </div>
        <p>默认先看状态与样本是否可比；详细图表保留在下方。</p>
      </header>
      <div className="compare-summary-strip__grid">
        <CompareSummaryCard state={stateA} side="left" />
        <CompareSummaryCard state={stateB} side="right" />
      </div>
    </section>
  )
}

function CompareSummaryCard({ state, side }: { state: NodeState; side: CompareSide }) {
  const label = sideLabel(side)
  if (state.loading) {
    return (
      <PageState
        kind="loading"
        title={`${label} 摘要读取中`}
        description="正在建立 24h runtime facts 摘要。"
        surface="empty"
        compact
        className="compare-summary-card compare-summary-card--state"
      />
    )
  }
  if (state.error || !state.node) {
    return (
      <PageState
        kind="error"
        title={`${label} 摘要不可用`}
        description="该侧节点无法生成摘要，详细指标会保持不可用状态。"
        technicalSummary={state.error ?? '节点不可用'}
        surface="empty"
        compact
        className="compare-summary-card compare-summary-card--state"
      />
    )
  }

  const node = state.node
  const sample = state.runtimeFacts?.latest_host_sample ?? null
  const sampleCount = state.runtimeFacts?.recent_host_samples.length ?? 0

  return (
    <article className="compare-summary-card" aria-label={`${label} 侧摘要`}>
      <header className="compare-summary-card__header">
        <span className="compare-summary-card__side">{label}</span>
        <div>
          <p>{label} 侧摘要</p>
          <h3>{node.display_name}</h3>
        </div>
      </header>
      <dl className="compare-summary-card__rows">
        <div className="compare-summary-row">
          <dt>健康状态</dt>
          <dd>
            <StatusGlyph state={nodeHealthGlyphState(node)} size="sm" ariaLabel={`${label} 健康状态`} />
            <StatusBadge label={node.current_health_status} />
          </dd>
        </div>
        <div className="compare-summary-row">
          <dt>生命周期</dt>
          <dd><StatusBadge label={node.lifecycle_status} /></dd>
        </div>
        <div className="compare-summary-row">
          <dt>运行 / 绑定</dt>
          <dd>
            <StatusBadge label={node.monitoring_status} />
            <StatusBadge label={node.binding_status} />
          </dd>
        </div>
        <div className="compare-summary-row compare-summary-row--stacked">
          <dt>位置上下文</dt>
          <dd>{nodeContext(node)}</dd>
        </div>
        <div className="compare-summary-row compare-summary-row--stacked">
          <dt>样本可用性</dt>
          <dd>
            <span className="compare-summary-row__sample">
              <StatusGlyph
                state={sample ? (sample.maintenance_context ? 'maintenance' : 'normal') : 'offline'}
                size="sm"
                ariaLabel={`${label} 样本状态`}
              />
              {sample ? '有样本' : '无样本'}
            </span>
            {sample ? (
              <span className="compare-summary-row__detail">
                窗口样本 <MonoDigits>{sampleCount}</MonoDigits> 条 · 最近观测{' '}
                <Timestamp value={sample.observed_at} mode="absolute" />
              </span>
            ) : (
              <span className="compare-summary-row__detail">24h runtime facts 暂无 HostSample</span>
            )}
          </dd>
        </div>
      </dl>
    </article>
  )
}

function CompareNodeIdentity({ state, side }: { state: NodeState; side: CompareSide }) {
  const label = sideLabel(side)
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
  return (
    <div className="compare-identity__card">
      <div className="compare-identity__header">
        <span className="compare-identity__side">{label}</span>
        <StatusGlyph
          state={nodeHealthGlyphState(node)}
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
        <span>{nodeContext(node)}</span>
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
      action={<Link className="btn sm ghost" to="/nodes">返回节点列表重新选择</Link>}
    />
  )
}
