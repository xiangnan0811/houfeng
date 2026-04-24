import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { StatusBadge } from '../components/StatusBadge'
import { ApiError, listNodes } from '../lib/api'
import { formatDateTime } from '../lib/format'
import type { NodeRecord } from '../lib/types'

export function NodesPage() {
  const [nodes, setNodes] = useState<NodeRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    listNodes()
      .then((result) => {
        if (cancelled) return
        setNodes(result)
        setLoading(false)
      })
      .catch((value: unknown) => {
        if (cancelled) return
        setError(value instanceof ApiError ? value.message : '加载节点列表失败')
        setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (loading) {
    return <section className="page-panel">正在加载节点列表…</section>
  }

  if (error) {
    return (
      <section className="page-panel">
        <h2 className="page-panel__title">节点</h2>
        <p className="page-panel__description">{error}</p>
      </section>
    )
  }

  return (
    <section className="page-stack">
      <header className="section-heading">
        <div>
          <p className="section-heading__eyebrow">Nodes</p>
          <h2 className="section-heading__title">节点列表</h2>
          <p className="section-heading__description">
            当前以“当前问题优先、最近运行事实次之”的冻结 V1 层级展示节点状态。
          </p>
        </div>
      </header>

      <div className="resource-table">
        <div className="resource-table__head">
          <span>节点</span>
          <span>状态</span>
          <span>最近心跳 / 同步</span>
          <span>当前主问题</span>
        </div>
        {nodes.map((node) => (
          <Link key={node.node_id} className="resource-table__row" to={`/nodes/${node.node_id}`}>
            <div>
              <strong>{node.display_name}</strong>
              <p>
                {node.region} · {node.city} · {node.provider}
              </p>
            </div>
            <div className="badge-row badge-row--wrap">
              <StatusBadge label={node.lifecycle_status} />
              <StatusBadge label={node.monitoring_status} />
              <StatusBadge label={node.current_health_status} />
            </div>
            <div>
              <strong>{formatDateTime(node.last_heartbeat_at)}</strong>
              <p>同步：{formatDateTime(node.last_sync_at)}</p>
            </div>
            <div>
              <strong>{node.current_active_incident_count}</strong>
              <p>{node.current_primary_issue_summary || '暂无明显异常'}</p>
            </div>
          </Link>
        ))}
      </div>
    </section>
  )
}
