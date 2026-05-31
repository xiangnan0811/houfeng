import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'

type VPSDetailHeroProps = {
  detail: VPSAssetDetail
  isArchived: boolean
  lifecycleSubmitting: boolean
  onDecisionEdit: () => void
  onCancellationOpen: () => void
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
  onDecisionEdit,
  onCancellationOpen,
  onFactEdit,
  onExperienceLog,
  onNodeLink,
  onServiceCreate,
  onDomainCreate,
  onArchiveStart,
  onRestoreStart,
}: VPSDetailHeroProps) {
  const location =
    [detail.country, detail.region, detail.city].filter(Boolean).join(' · ') || '位置未确认'
  return (
    <header className="watchtower-header" role="banner" aria-label="VPS 身份与操作">
      <div className="watchtower-header__row1">
        <div className="watchtower-header__title-block">
          <h1>{detail.display_name}</h1>
          <div className="badge-row">
            <LifecycleBadge value={detail.lifecycle_status} />
            <UsageBadge value={detail.usage_status} />
            <RenewalBadge value={detail.renewal_decision} />
            <Badge variant="count" tone="neutral">{detail.active_node_link_count} 个 Node</Badge>
          </div>
        </div>
        <div className="watchtower-header__actions-block">
          <div className="watchtower-header__actions">
            <Button variant="primary" size="sm" onClick={onDecisionEdit}>处理决策</Button>
            <Button variant="danger" size="sm" onClick={onCancellationOpen}>取消/退役</Button>
            <details className="watchtower-actions-menu vps-detail-actions-menu">
              <summary aria-label="VPS 详情操作">…</summary>
              <div className="watchtower-actions-menu__panel">
                <button type="button" onClick={onCancellationOpen}>取消/退役工作台</button>
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
            <Link className="btn sm ghost" to="/vps">VPS 列表</Link>
          </div>
        </div>
      </div>
      <div className="watchtower-header__row2">
        <span className="watchtower-header__meta-item">{formatOptional(detail.provider_name)}</span>
        <span className="watchtower-header__meta-sep" aria-hidden>·</span>
        <span className="watchtower-header__meta-item">{location}</span>
      </div>
    </header>
  )
}
