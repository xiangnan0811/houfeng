import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSAssetDetail } from '../../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'
import { VPSAssetMark } from './VPSAssetMark'
import { VPSIdentityMeta } from './VPSOverviewIdentityHeader'
import { vpsIdentityMetaFields } from './vpsDetailResourcePresentation'

type VPSDetailHeroProps = {
  detail: VPSAssetDetail
  isArchived: boolean
  showCancellationWorkbench: boolean
  lifecycleSubmitting: boolean
  monitoringAgentActionLabel: string
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
  monitoringAgentActionLabel,
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
    <header className="page__head vps-overview-identity" role="banner" aria-label="VPS 身份与操作">
      <div className="vps-overview-identity__lead">
        <VPSAssetMark />
        <div className="vps-overview-identity__copy">
          <h1 className="page__title">{detail.display_name}</h1>
          <div className="vps-overview-identity__statuses" role="group" aria-label="VPS 当前状态">
            <span className="vps-overview-identity__status vps-overview-identity__status--lifecycle">
              <LifecycleBadge value={detail.lifecycle_status} />
            </span>
            <span className="vps-overview-identity__status vps-overview-identity__status--usage">
              <UsageBadge value={detail.usage_status} />
            </span>
            <span className="vps-overview-identity__status vps-overview-identity__status--decision">
              <RenewalBadge value={detail.renewal_decision} />
            </span>
            <Badge variant="count" tone="neutral">{detail.active_monitoring_instance_link_count} 个监控实例</Badge>
          </div>
          <VPSIdentityMeta
            items={vpsIdentityMetaFields({
              vpsId: detail.vps_id,
              ...(detail.provider_name.trim() ? { providerName: formatOptional(detail.provider_name) } : {}),
              ...(location ? { location } : {}),
              ...(detail.ipv4.trim() ? { ipv4: detail.ipv4 } : {}),
              ...(detail.updated_at ? { updatedAt: detail.updated_at } : {}),
            })}
          />
        </div>
      </div>
      <div className="page__actions">
            <Button variant="primary" size="sm" onClick={onDecisionEdit}>续费决策</Button>
            <Link className="btn sm secondary" to={`/asset-decisions?view=needs_decision&renew_within_days=30&vps_id=${encodeURIComponent(detail.vps_id)}`}>
              组合决策
            </Link>
            <Button variant="secondary" size="sm" onClick={onSubscriptionCreate}>创建订阅</Button>
            <Button variant="secondary" size="sm" onClick={onValidityExtend}>延长有效期</Button>
            <Button variant="secondary" size="sm" onClick={onMonitoringInstanceCreate}>{monitoringAgentActionLabel}</Button>
            <details className="watchtower-actions-menu vps-detail-actions-menu">
              <summary aria-label="VPS 详情操作">…</summary>
              <div className="watchtower-actions-menu__panel">
                <button type="button" onClick={onFactEdit}>编辑事实</button>
                <button type="button" onClick={onExperienceLog}>记录经验</button>
                <button type="button" onClick={onSubscriptionCreate}>快速创建订阅</button>
                <button type="button" onClick={onValidityExtend}>延长有效期</button>
                <button type="button" onClick={onMonitoringInstanceCreate}>{monitoringAgentActionLabel}</button>
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
      </div>
    </header>
  )
}
