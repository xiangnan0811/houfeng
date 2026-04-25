import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { IncidentList } from '../components/IncidentList'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  getNode,
  getNodeRuntimeFacts,
  listEvents,
  listIncidents,
} from '../lib/api'
import {
  formatBytes,
  formatBytesPerSecond,
  formatDateTime,
  formatLabelList,
  formatNumber,
  formatPercent,
  formatUptime,
} from '../lib/format'
import type {
  ActiveIncidentRecord,
  NodeRecord,
  NodeRuntimeFacts,
  StateChangeEventRecord,
} from '../lib/types'

type State = {
  requestedNodeId: string | null
  error: string | null
  node: NodeRecord | null
  runtimeFacts: NodeRuntimeFacts | null
  incidents: ActiveIncidentRecord[]
  events: StateChangeEventRecord[]
}

export function NodeDetailPage() {
  const { nodeId } = useParams()
  const [state, setState] = useState<State>({
    requestedNodeId: null,
    error: null,
    node: null,
    runtimeFacts: null,
    incidents: [],
    events: [],
  })

  useEffect(() => {
    let cancelled = false
    if (!nodeId) return

    Promise.all([
      getNode(nodeId),
      getNodeRuntimeFacts(nodeId),
      listIncidents({ object_type: 'node', object_id: nodeId }),
      listEvents({ object_type: 'node', object_id: nodeId }),
    ])
      .then(([node, runtimeFacts, incidents, events]) => {
        if (cancelled) return
        setState({
          requestedNodeId: nodeId,
          error: null,
          node,
          runtimeFacts,
          incidents,
          events,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '节点不存在'
            : error instanceof Error
              ? error.message
              : '加载节点详情失败'
        setState({
          requestedNodeId: nodeId,
          error: message,
          node: null,
          runtimeFacts: null,
          incidents: [],
          events: [],
        })
      })

    return () => {
      cancelled = true
    }
  }, [nodeId])

  const missingNodeId = !nodeId
  const isCurrentNode = state.requestedNodeId === nodeId
  const error = isCurrentNode ? state.error : null
  const node = isCurrentNode ? state.node : null
  const runtimeFacts = isCurrentNode ? state.runtimeFacts : null
  const incidents = isCurrentNode ? state.incidents : []
  const events = isCurrentNode ? state.events : []

  if (!missingNodeId && !isCurrentNode) {
    return <section className="page-panel">正在加载节点详情…</section>
  }

  if (missingNodeId || error || !node) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Node Detail</p>
        <h2 className="page-panel__title">节点详情不可用</h2>
        <p className="page-panel__description">{error ?? '未找到节点'}</p>
        <Link className="text-link" to="/nodes">
          返回节点列表
        </Link>
      </section>
    )
  }

  const sample = runtimeFacts?.latest_host_sample ?? null

  return (
    <div className="page-stack">
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">Node Detail</p>
          <h2 className="hero-panel__title">{node.display_name}</h2>
          <p className="hero-panel__description">
            {node.region} · {node.city} · {node.provider}
          </p>
          <div className="badge-row">
            <StatusBadge label={node.lifecycle_status} />
            <StatusBadge label={node.monitoring_status} />
            <StatusBadge label={node.binding_status} />
            <StatusBadge label={node.current_health_status} />
          </div>
        </div>
        <div className="hero-panel__meta">
          <div className="hero-meta-card">
            <span>标签</span>
            <strong>{formatLabelList(node.labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近心跳</span>
            <strong>{formatDateTime(node.last_heartbeat_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近同步</span>
            <strong>{formatDateTime(node.last_sync_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>当前主问题</span>
            <strong>{node.current_primary_issue_summary || '暂无明显异常'}</strong>
          </div>
        </div>
      </section>

      <div className="summary-grid">
        <article className="summary-card">
          <p className="summary-card__label">健康状态</p>
          <p className="summary-card__value">{node.current_health_status}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">活跃异常数</p>
          <p className="summary-card__value">{node.current_active_incident_count}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">当前主问题</p>
          <p className="summary-card__value summary-card__value--text">
            {node.current_primary_issue_summary || '暂无明显异常'}
          </p>
        </article>
      </div>

      <DetailSection
        eyebrow="Current Runtime Facts"
        title="当前主机指标"
        aside={sample ? `样本时间：${formatDateTime(sample.observed_at)}` : '等待首批样本'}
      >
        {sample ? (
          <div className="metric-grid">
            <article className="metric-card">
              <h3>CPU / Load</h3>
              <dl>
                <div>
                  <dt>CPU 使用率</dt>
                  <dd>{formatPercent(sample.cpu_usage_pct)}</dd>
                </div>
                <div>
                  <dt>Load 1 / 5 / 15</dt>
                  <dd>
                    {formatNumber(sample.load_1)} / {formatNumber(sample.load_5)} /{' '}
                    {formatNumber(sample.load_15)}
                  </dd>
                </div>
                <div>
                  <dt>iowait / steal</dt>
                  <dd>
                    {formatPercent(sample.cpu_iowait_pct)} /{' '}
                    {formatPercent(sample.cpu_steal_pct)}
                  </dd>
                </div>
              </dl>
            </article>

            <article className="metric-card">
              <h3>内存 / Swap</h3>
              <dl>
                <div>
                  <dt>内存使用率</dt>
                  <dd>{formatPercent(sample.mem_used_pct)}</dd>
                </div>
                <div>
                  <dt>可用内存</dt>
                  <dd>{formatBytes(sample.mem_available_bytes)}</dd>
                </div>
                <div>
                  <dt>Swap 使用率</dt>
                  <dd>{formatPercent(sample.swap_used_pct)}</dd>
                </div>
              </dl>
            </article>

            <article className="metric-card">
              <h3>磁盘 / Inode</h3>
              <dl>
                <div>
                  <dt>磁盘使用率</dt>
                  <dd>{formatPercent(sample.disk_used_pct)}</dd>
                </div>
                <div>
                  <dt>Inode 使用率</dt>
                  <dd>{formatPercent(sample.inode_used_pct)}</dd>
                </div>
                <div>
                  <dt>磁盘繁忙度</dt>
                  <dd>{formatPercent(sample.disk_busy_pct)}</dd>
                </div>
              </dl>
            </article>

            <article className="metric-card">
              <h3>网络 / 吞吐</h3>
              <dl>
                <div>
                  <dt>流入 / 流出</dt>
                  <dd>
                    {formatBytesPerSecond(sample.net_in_bytes_per_sec)} /{' '}
                    {formatBytesPerSecond(sample.net_out_bytes_per_sec)}
                  </dd>
                </div>
                <div>
                  <dt>磁盘读 / 写</dt>
                  <dd>
                    {formatBytesPerSecond(sample.disk_read_bytes_per_sec)} /{' '}
                    {formatBytesPerSecond(sample.disk_write_bytes_per_sec)}
                  </dd>
                </div>
                <div>
                  <dt>运行时长</dt>
                  <dd>{formatUptime(sample.uptime_seconds)}</dd>
                </div>
              </dl>
            </article>
          </div>
        ) : (
          <div className="empty-state">
            <h3>尚未收到主机样本</h3>
            <p>该节点已存在，但首批 HostSample 还未到达。请等待下一次 agent 同步。</p>
          </div>
        )}
      </DetailSection>

      <DetailSection eyebrow="Incidents" title="当前活跃异常">
        <IncidentList incidents={incidents} />
      </DetailSection>

      <DetailSection eyebrow="Events" title="最近相关事件">
        <EventList events={events} />
      </DetailSection>
    </div>
  )
}
