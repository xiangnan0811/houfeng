import { StatusBadge } from '../StatusBadge'
import { Timestamp } from '../atoms'
import { formatLabelList } from '../../lib/format'
import type { NodeRecord } from '../../lib/types'

type NodeHeroProps = {
  node: NodeRecord
}

export function NodeHero({ node }: NodeHeroProps) {
  return (
    <section className="hero-panel">
      <div className="hero-panel__content">
        <p className="hero-panel__eyebrow">节点详情</p>
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
          <strong>
            <Timestamp value={node.last_heartbeat_at} mode="both" />
          </strong>
        </div>
        <div className="hero-meta-card">
          <span>最近同步</span>
          <strong>
            <Timestamp value={node.last_sync_at} mode="both" />
          </strong>
        </div>
        <div className="hero-meta-card">
          <span>当前主问题</span>
          <strong>{node.current_primary_issue_summary || '暂无明显异常'}</strong>
        </div>
      </div>
    </section>
  )
}
