import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'

type VPSDetailHeroProps = {
  detail: VPSAssetDetail
  onBack: () => void
  onDecisionEdit: () => void
  onFactEdit: () => void
}

export function VPSDetailHero({
  detail,
  onBack,
  onDecisionEdit,
  onFactEdit,
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
      <div className="page-panel__actions">
        <Button variant="primary" onClick={onDecisionEdit}>处理决策</Button>
        <Button variant="secondary" onClick={onFactEdit}>编辑资料</Button>
        <Button variant="ghost" onClick={onBack}>返回</Button>
        <Link className="btn btn--ghost btn--md" to="/vps">VPS 列表</Link>
      </div>
    </section>
  )
}
