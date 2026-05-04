import { StatusBadge } from '../StatusBadge'
import { Hostname, Timestamp } from '../atoms'
import { formatLabelList } from '../../lib/format'
import type { TargetRecord } from '../../lib/types'

type TargetHeroProps = {
  target: TargetRecord
}

export function TargetHero({ target }: TargetHeroProps) {
  const hostDisplay = target.base_port
    ? `${target.host}:${target.base_port}`
    : target.host
  return (
    <section className="hero-panel">
      <div className="hero-panel__content">
        <p className="hero-panel__eyebrow">目标详情</p>
        <h2 className="hero-panel__title">{target.name}</h2>
        <p className="hero-panel__description">
          {target.target_type} · <Hostname>{hostDisplay}</Hostname>
        </p>
        <div className="badge-row">
          <StatusBadge label={target.run_status} />
          <StatusBadge label={target.current_health_status} />
          <StatusBadge label={target.target_type} />
        </div>
      </div>
      <div className="hero-panel__meta">
        <div className="hero-meta-card">
          <span>标签</span>
          <strong>{formatLabelList(target.labels)}</strong>
        </div>
        <div className="hero-meta-card">
          <span>执行节点标签</span>
          <strong>{formatLabelList(target.execution_node_labels)}</strong>
        </div>
        <div className="hero-meta-card">
          <span>最近成功</span>
          <strong>
            <Timestamp value={target.last_success_at ?? null} mode="both" />
          </strong>
        </div>
        <div className="hero-meta-card">
          <span>最近失败</span>
          <strong>
            <Timestamp value={target.last_failure_at ?? null} mode="both" />
          </strong>
        </div>
      </div>
    </section>
  )
}
