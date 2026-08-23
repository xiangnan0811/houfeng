import { Link } from 'react-router-dom'
import type { RefObject } from 'react'

import { Button } from '../../components/atoms'
import type { VPSOverview } from '../../lib/types'
import { subjectNewRecordHref } from '../records/activity/activityQueryState'
import { SubjectLocalNavigation } from '../records/activity/SubjectLocalNavigation'
import { VPSManagementMenu } from './VPSManagementMenu'
import { VPSOverviewAnomalies } from './VPSOverviewAnomalies'
import { VPSOverviewFacts } from './VPSOverviewFacts'
import { VPSOverviewIdentityHeader } from './VPSOverviewIdentityHeader'
import { VPSOverviewRecentActivity } from './VPSOverviewRecentActivity'
import { VPSOverviewRelations } from './VPSOverviewRelations'
import { VPSOverviewSummaryGrid } from './VPSOverviewSummaryGrid'
import type { VPSManagementController } from './hooks/useVPSManagementController'

type Props = {
  overview: VPSOverview
  management: VPSManagementController
  managementTriggerRef?: RefObject<HTMLButtonElement | null> | undefined
  onRefresh: () => void
}

export function VPSOverviewPageView({
  overview,
  management,
  managementTriggerRef,
  onRefresh,
}: Props) {
  const vpsId = overview.identity.vps_id
  const basePath = `/vps/${vpsId}`
  const activityHref = `${basePath}/activity`
  const newRecordHref = subjectNewRecordHref({
    kind: 'vps',
    sourceId: vpsId,
    view: 'activity',
    basePath,
  })
  const subject = {
    kind: 'vps' as const,
    sourceId: vpsId,
    view: 'activity' as const,
    basePath,
  }

  return (
    <div className="page-stack vps-overview-page">
      <div className="vps-overview-page__identity-wrap">
        <VPSOverviewIdentityHeader
          identity={overview.identity}
          newRecordHref={newRecordHref}
          timelineHref={activityHref}
          {...(managementTriggerRef ? { managementTriggerRef } : {})}
          onManage={() => {
            if (management.menuOpen) management.closeMenu()
            else management.openMenu()
          }}
        />
        <VPSManagementMenu
          controller={management}
          {...(managementTriggerRef ? { returnFocusRef: managementTriggerRef } : {})}
        />
      </div>

      <SubjectLocalNavigation
        subject={subject}
        activeView="activity"
        overviewHref={basePath}
        overviewCurrent
      />

      {/* Anomalies only mount when present — healthy DOM has zero anomaly nodes. */}
      {overview.anomalies.length > 0 ? (
        <VPSOverviewAnomalies anomalies={overview.anomalies} />
      ) : null}

      <VPSOverviewSummaryGrid summary={overview.summary} />
      <VPSOverviewRecentActivity
        items={overview.recent_activity.items}
        activityHref={activityHref}
      />
      <VPSOverviewFacts facts={overview.facts} />
      <VPSOverviewRelations relations={overview.relations} />

      {overview.recent_activity.section.state === 'unavailable'
        || overview.summary.monitoring.section.state === 'unavailable' ? (
        <p className="vps-overview-page__section-note" role="status">
          部分区段暂不可用。
          <Button type="button" size="sm" variant="ghost" onClick={onRefresh}>
            重试
          </Button>
          <Link className="text-link" to={activityHref}>打开时间线</Link>
        </p>
      ) : null}

    </div>
  )
}
