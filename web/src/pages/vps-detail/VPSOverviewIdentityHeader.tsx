import { Link } from 'react-router-dom'

import { Button } from '../../components/atoms'
import type { VPSOverviewIdentity } from '../../lib/types'

type Props = {
  identity: VPSOverviewIdentity
  onManage: () => void
  newRecordHref: string
  timelineHref: string
}

export function VPSOverviewIdentityHeader({
  identity,
  onManage,
  newRecordHref,
  timelineHref,
}: Props) {
  return (
    <header className="vps-overview-identity">
      <div className="vps-overview-identity__lead">
        <p className="vps-overview-identity__eyebrow">VPS</p>
        <h1 className="vps-overview-identity__title">{identity.display_name || identity.vps_id}</h1>
        <p className="vps-overview-identity__meta">
          <span className="mono">{identity.vps_id}</span>
          <span>{identity.provider_name}</span>
          <span>{identity.lifecycle_status}</span>
        </p>
      </div>
      <div className="vps-overview-identity__actions" aria-label="VPS 首层动作">
        <Link className="btn sm primary" to={newRecordHref}>新建记录</Link>
        <Link className="btn sm secondary" to={timelineHref}>时间线</Link>
        <Button type="button" size="sm" variant="secondary" onClick={onManage}>
          管理
        </Button>
      </div>
    </header>
  )
}
