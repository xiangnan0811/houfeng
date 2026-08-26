import { useId, type RefObject } from 'react'

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
import type { VPSOverviewCommand } from './vpsOverviewDestination'

type Props = {
  overview: VPSOverview
  management: VPSManagementController
  managementTriggerRef?: RefObject<HTMLButtonElement | null> | undefined
  onRefresh: () => void
  retrying: boolean
  refreshError?: string | null
}

export function VPSOverviewPageView({
  overview,
  management,
  managementTriggerRef,
  onRefresh,
  retrying,
  refreshError = null,
}: Props) {
  const managementMenuId = useId()
  const vpsId = overview.identity.vps_id
  const readonlyLifecycle = overview.identity.lifecycle_status === 'cancelled'
    || overview.identity.lifecycle_status === 'archived'
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

  function runCommand(command: VPSOverviewCommand) {
    switch (command) {
      case 'open_subscription':
        management.openPanel('subscription')
        return
      case 'open_renewal_decision':
        management.openPanel('decision')
        return
      case 'open_management':
        if (
          overview.identity.lifecycle_status === 'to_cancel'
          || overview.identity.lifecycle_status === 'to_migrate'
        ) {
          management.openPanel('cancellation')
          return
        }
        management.openMenu()
        return
      case 'retry_overview':
        onRefresh()
        return
      case 'open_monitoring_instances':
        management.openPanel('monitoring-instance-evidence')
        return
      case 'open_services':
        management.openPanel('services-detail')
        return
      case 'open_domains':
        management.openPanel('domains-detail')
    }
  }

  return (
    <div className="page-stack vps-overview-page">
      <div className="vps-overview-page__identity-wrap">
        <VPSOverviewIdentityHeader
          identity={overview.identity}
          {...(readonlyLifecycle ? {} : { newRecordHref })}
          timelineHref={activityHref}
          {...(managementTriggerRef && !readonlyLifecycle ? { managementTriggerRef } : {})}
          menuOpen={management.menuOpen}
          menuId={managementMenuId}
          {...(readonlyLifecycle ? {} : {
            onManage: () => {
              if (management.menuOpen) management.closeMenu()
              else management.openMenu()
            },
          })}
        />
        {readonlyLifecycle ? null : (
        <VPSManagementMenu
          lifecycleStatus={overview.identity.lifecycle_status}
          renewalDecision={overview.identity.renewal_decision}
          controller={management}
          menuId={managementMenuId}
          {...(managementTriggerRef ? { returnFocusRef: managementTriggerRef } : {})}
        />
        )}
      </div>

      <SubjectLocalNavigation
        subject={subject}
        activeView="activity"
        overviewHref={basePath}
        overviewCurrent
      />

      {refreshError ? (
        <p className="create-form__error" role="status">
          本次刷新失败，当前仍展示上次成功数据。{refreshError}
        </p>
      ) : null}

      {/* Anomalies only mount when present — healthy DOM has zero anomaly nodes. */}
      {overview.anomalies.length > 0 ? (
        <VPSOverviewAnomalies vpsId={vpsId} anomalies={overview.anomalies} onCommand={runCommand} />
      ) : null}

      <VPSOverviewSummaryGrid summary={overview.summary} onRefresh={onRefresh} retrying={retrying} />
      <VPSOverviewRecentActivity
        items={overview.recent_activity.items}
        activityHref={activityHref}
        section={overview.recent_activity.section}
        onRefresh={onRefresh}
        retrying={retrying}
      />
      <VPSOverviewFacts facts={overview.facts} />
      <VPSOverviewRelations
        vpsId={vpsId}
        relations={overview.relations}
        onCommand={runCommand}
        onRefresh={onRefresh}
        retrying={retrying}
      />

    </div>
  )
}
