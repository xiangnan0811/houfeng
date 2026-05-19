import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'

type VPSDetailHeroProps = {
  detail: VPSAssetDetail
  isArchived: boolean
  lifecycleSubmitting: boolean
  onBack: () => void
  onDecisionEdit: () => void
  onFactEdit: () => void
  onExperienceLog: () => void
  onNodeLink: () => void
  onServiceCreate: () => void
  onDomainCreate: () => void
  onArchiveStart: () => void
  onRestoreStart: () => void
}

export function VPSDetailHero({
  detail,
  isArchived,
  lifecycleSubmitting,
  onBack,
  onDecisionEdit,
  onFactEdit,
  onExperienceLog,
  onNodeLink,
  onServiceCreate,
  onDomainCreate,
  onArchiveStart,
  onRestoreStart,
}: VPSDetailHeroProps) {
  return (
    <section className="page-panel page-panel--inline vps-detail-hero">
      <div>
        <div className="page-panel__eyebrow">VPS DETAIL</div>
        <h1 className="page-panel__title">{detail.display_name}</h1>
        <p className="page-panel__description">
          {formatOptional(detail.provider_name)} · {[detail.country, detail.region, detail.city].filter(Boolean).join(' · ') || '位置未确认'}
        </p>
        <div className="asset-hero-meta">
          <LifecycleBadge value={detail.lifecycle_status} />
          <UsageBadge value={detail.usage_status} />
          <RenewalBadge value={detail.renewal_decision} />
          <Badge variant="count" tone="neutral">{detail.active_node_link_count} 个 Node</Badge>
        </div>
      </div>
      <div className="page-panel__actions vps-detail-hero__actions">
        <Button variant="primary" onClick={onDecisionEdit}>处理决策</Button>
        <details className="watchtower-actions-menu vps-detail-actions-menu">
          <summary aria-label="VPS 详情操作">…</summary>
          <div className="watchtower-actions-menu__panel">
            <button type="button" onClick={onFactEdit}>编辑基础信息</button>
            <button type="button" onClick={onExperienceLog}>记录经验</button>
            <button type="button" onClick={onNodeLink}>关联 Node</button>
            <button type="button" onClick={onServiceCreate}>新增服务</button>
            <button type="button" onClick={onDomainCreate}>新增域名</button>
            {isArchived ? (
              <button type="button" disabled={lifecycleSubmitting} onClick={onRestoreStart}>
                {lifecycleSubmitting ? '恢复中…' : '恢复为闲置'}
              </button>
            ) : (
              <button
                type="button"
                className="watchtower-actions-menu__danger"
                disabled={lifecycleSubmitting}
                onClick={onArchiveStart}
              >
                {lifecycleSubmitting ? '归档中…' : '归档 VPS'}
              </button>
            )}
          </div>
        </details>
        <Button variant="ghost" onClick={onBack}>返回</Button>
        <Link className="btn btn--ghost btn--md" to="/vps">VPS 列表</Link>
      </div>
    </section>
  )
}
