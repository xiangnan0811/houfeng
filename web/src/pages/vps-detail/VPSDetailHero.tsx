import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'

type VPSDetailHeroProps = {
  detail: VPSAssetDetail
  isArchived: boolean
  showCancellationWorkbench: boolean
  lifecycleSubmitting: boolean
  onDecisionEdit: () => void
  onCancellationOpen: () => void
  onFactEdit: () => void
  onExperienceLog: () => void
  onMonitoringInstanceCreate: () => void
  onMonitoringInstanceLink: () => void
  onSubscriptionCreate: () => void
  onValidityExtend: () => void
  onServiceCreate: () => void
  onDomainCreate: () => void
  onArchiveStart: () => void
  onRestoreStart: () => void
}

export function VPSDetailHero({
  detail,
  isArchived,
  showCancellationWorkbench,
  lifecycleSubmitting,
  onDecisionEdit,
  onCancellationOpen,
  onFactEdit,
  onExperienceLog,
  onMonitoringInstanceCreate,
  onMonitoringInstanceLink,
  onSubscriptionCreate,
  onValidityExtend,
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
            <Badge variant="count" tone="neutral">{detail.active_monitoring_instance_link_count} 个监控实例</Badge>
          </div>
        </div>
        <div className="watchtower-header__actions-block">
          <div className="watchtower-header__actions">
            <Button variant="primary" size="sm" onClick={onDecisionEdit}>处理决策</Button>
            <Link className="btn sm secondary" to={`/asset-decisions?view=needs_decision&renew_within_days=30&vps_id=${encodeURIComponent(detail.vps_id)}`}>
              组合决策
            </Link>
            <Button variant="secondary" size="sm" onClick={onSubscriptionCreate}>创建订阅</Button>
            <Button variant="secondary" size="sm" onClick={onValidityExtend}>延长有效期</Button>
            <Button variant="secondary" size="sm" onClick={onMonitoringInstanceCreate}>接入 agent</Button>
            <details className="watchtower-actions-menu vps-detail-actions-menu">
              <summary aria-label="VPS 详情操作">…</summary>
              <div className="watchtower-actions-menu__panel">
                <button type="button" onClick={onFactEdit}>编辑基础信息</button>
                <button type="button" onClick={onExperienceLog}>记录经验</button>
                <button type="button" onClick={onSubscriptionCreate}>快速创建订阅</button>
                <button type="button" onClick={onValidityExtend}>延长有效期</button>
                <button type="button" onClick={onMonitoringInstanceCreate}>创建并接入 agent</button>
                <button type="button" onClick={onMonitoringInstanceLink}>关联已有监控实例</button>
                <button type="button" onClick={onServiceCreate}>新增服务</button>
                <button type="button" onClick={onDomainCreate}>新增域名</button>
                {showCancellationWorkbench ? (
                  <button type="button" onClick={onCancellationOpen}>取消/退役工作台</button>
                ) : null}
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
