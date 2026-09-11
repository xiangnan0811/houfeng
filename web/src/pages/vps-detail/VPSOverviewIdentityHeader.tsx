import { Link } from 'react-router-dom'
import type { Ref } from 'react'

import { Badge, Button, Timestamp } from '../../components/atoms'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import type { VPSOverviewIdentity } from '../../lib/types'
import { overviewLocationLabel } from '../../lib/vpsOverviewPresentation'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'
import { VPSAssetMark } from './VPSAssetMark'
import { vpsIdentityMetaFields, type VPSIdentityMetaField } from './vpsDetailResourcePresentation'

export function VPSIdentityMeta({ items }: { items: VPSIdentityMetaField[] }) {
  if (items.length === 0) return null
  return (
    <dl className="vps-overview-identity__meta">
      {items.map((item) => (
        <div key={item.label} className="vps-overview-identity__meta-item">
          <dt>{item.label}</dt>
          <dd className={item.mono ? 'mono' : ''}>
            {item.timestamp ? <Timestamp value={item.value} mode="absolute" /> : item.value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

type Props = {
  identity: VPSOverviewIdentity
  onManage?: () => void
  newRecordHref?: string
  managementTriggerRef?: Ref<HTMLButtonElement>
  menuOpen?: boolean
  menuId?: string
}

export function VPSOverviewIdentityHeader({
  identity,
  onManage,
  newRecordHref,
  managementTriggerRef,
  menuOpen = false,
  menuId,
}: Props) {
  const location = overviewLocationLabel([identity.country, identity.region, identity.city])
  const ipv4 = identity.ipv4.trim()
  const meta = vpsIdentityMetaFields({
    vpsId: identity.vps_id,
    ...(identity.provider_name.trim() ? { providerName: identity.provider_name } : {}),
    ...(location ? { location } : {}),
    ...(ipv4 ? { ipv4 } : {}),
    ...(identity.updated_at ? { updatedAt: identity.updated_at } : {}),
  })

  return (
    <header className="page__head vps-overview-identity">
      <div className="vps-overview-identity__lead">
        <VPSAssetMark />
        <div className="vps-overview-identity__copy">
          <div className="vps-overview-identity__title-row">
            <h1 className="page__title">{identity.display_name || identity.vps_id}</h1>
            {READ_ONLY_PREVIEW ? <Badge variant="info" className="vps-overview-identity__readonly">只读预览</Badge> : null}
          </div>
          <div className="vps-overview-identity__statuses" role="group" aria-label="VPS 当前状态">
            <span className="vps-overview-identity__status vps-overview-identity__status--lifecycle">
              <LifecycleBadge value={identity.lifecycle_status} />
            </span>
            <span className="vps-overview-identity__status vps-overview-identity__status--usage">
              <UsageBadge value={identity.usage_status} />
            </span>
            <span className="vps-overview-identity__status vps-overview-identity__status--decision">
              <RenewalBadge value={identity.renewal_decision} />
            </span>
          </div>
          <VPSIdentityMeta items={meta} />
        </div>
      </div>
      {newRecordHref || onManage ? (
        <div className="page__actions" role="group" aria-label="VPS 首层动作">
          {newRecordHref ? (
            <Link className="btn secondary" to={newRecordHref}>新建记录</Link>
          ) : null}
          {onManage ? (
            <Button
              ref={managementTriggerRef}
              type="button"
              variant="primary"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              {...(menuId ? { 'aria-controls': menuId } : {})}
              onClick={onManage}
            >
              管理
            </Button>
          ) : null}
        </div>
      ) : null}
    </header>
  )
}
