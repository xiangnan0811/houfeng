import { Link } from 'react-router-dom'
import type { Ref } from 'react'

import { Button, Timestamp } from '../../components/atoms'
import type { VPSOverviewIdentity } from '../../lib/types'
import {
  overviewLifecycleLabel,
  overviewLocationLabel,
  overviewUsageLabel,
} from '../../lib/vpsOverviewPresentation'

type Props = {
  identity: VPSOverviewIdentity
  onManage?: () => void
  newRecordHref?: string
  timelineHref: string
  managementTriggerRef?: Ref<HTMLButtonElement>
  menuOpen?: boolean
  menuId?: string
}

export function VPSOverviewIdentityHeader({
  identity,
  onManage,
  newRecordHref,
  timelineHref,
  managementTriggerRef,
  menuOpen = false,
  menuId,
}: Props) {
  const location = overviewLocationLabel([identity.country, identity.region, identity.city])

  return (
    <header className="vps-overview-identity">
      <div className="vps-overview-identity__lead">
        <p className="vps-overview-identity__eyebrow">VPS</p>
        <h1 className="vps-overview-identity__title">{identity.display_name || identity.vps_id}</h1>
        <p className="vps-overview-identity__meta">
          <span className="mono">{identity.vps_id}</span>
          <span>{identity.provider_name}</span>
          {location ? <span>{location}</span> : null}
          <span>{overviewUsageLabel(identity.usage_status)}</span>
          <span>{overviewLifecycleLabel(identity.lifecycle_status)}</span>
          <span>
            更新 <Timestamp value={identity.updated_at} mode="absolute" />
          </span>
        </p>
      </div>
      <div className="vps-overview-identity__actions" aria-label="VPS 首层动作">
        {newRecordHref ? <Link className="btn lg primary" to={newRecordHref}>新建记录</Link> : null}
        <Link className="btn lg secondary" to={timelineHref}>时间线</Link>
        {onManage ? (
        <Button
          ref={managementTriggerRef}
          type="button"
          size="lg"
          variant="secondary"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          {...(menuId ? { 'aria-controls': menuId } : {})}
          onClick={onManage}
        >
          管理
        </Button>
        ) : null}
      </div>
    </header>
  )
}
