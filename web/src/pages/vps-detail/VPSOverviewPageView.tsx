import { useId, type RefObject } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Button } from '../../components/atoms'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import type { VPSOverview } from '../../lib/types'
import {
  overviewIPQualityActionLabel,
  overviewLifecycleLabel,
  overviewSummaryCellLabel,
} from '../../lib/vpsOverviewPresentation'


import { subjectNewRecordHref } from '../records/activity/activityQueryState'
import { SubjectLocalNavigation } from '../records/activity/SubjectLocalNavigation'
import { VPSDetailResourceList } from './VPSDetailResourceList'
import {
  domainResourceExtra,
  domainResourceName,
  domainResourceStatus,
  httpHref,
  serviceResourceName,
  serviceResourceStatus,
  serviceResourceType,
} from './vpsDetailResourcePresentation'
import { VPSDetailSectionNav } from './VPSDetailSectionNav'
import { VPSManagementMenu } from './VPSManagementMenu'
import { VPSOverviewAnomalies } from './VPSOverviewAnomalies'
import { VPSOverviewFacts } from './VPSOverviewFacts'
import { VPSOverviewFreshness } from './VPSOverviewFreshness'
import { VPSOverviewIdentityHeader } from './VPSOverviewIdentityHeader'
import { VPSOverviewRecentActivity } from './VPSOverviewRecentActivity'
import { VPSOverviewSummaryGrid } from './VPSOverviewSummaryGrid'
import { VPSSubscriptionOpsBody } from './VPSSubscriptionOpsBody'
import type { VPSManagementController } from './hooks/useVPSManagementController'
import { useVPSDetailResources } from './hooks/useVPSDetailResources'
import { isVPSOverviewWriteCommand, type VPSOverviewCommand } from './vpsOverviewDestination'


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
  const location = useLocation()
  const managementMenuId = useId()
  const resources = useVPSDetailResources(overview)
  const vpsId = overview.identity.vps_id
  const readonlyLifecycle = overview.identity.lifecycle_status === 'cancelled'
    || overview.identity.lifecycle_status === 'archived'
  const hideCreate = readonlyLifecycle || READ_ONLY_PREVIEW
  const hideManage = readonlyLifecycle || READ_ONLY_PREVIEW

  const basePath = `/vps/${vpsId}`
  const activityHref = `${basePath}/activity`
  const ipQualityHref = `${basePath}/ip-quality`
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
  const monitoringRelations = overview.relations.filter((relation) => relation.kind === 'monitoring_instances')
  const monitoringCount = monitoringRelations[0]?.count ?? 0
  const subscriptionsHref = `/subscriptions?${new URLSearchParams({ vps_id: vpsId }).toString()}`
  const primarySubscription = resources.subscriptions.items[0]
  const plannedCancellation = overview.identity.lifecycle_status === 'to_cancel'

  function runCommand(command: VPSOverviewCommand) {
    if (READ_ONLY_PREVIEW && isVPSOverviewWriteCommand(command)) return
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
      case 'open_monitoring_onboarding':
        management.openPanel('monitoring-instance-create')
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
    <div className="page vps-overview-page vps-detail-workspace">
      <div className="vps-overview-page__identity-wrap">
        <VPSOverviewIdentityHeader
          identity={overview.identity}
          {...(hideCreate ? {} : { newRecordHref })}
          {...(managementTriggerRef && !hideManage ? { managementTriggerRef } : {})}
          menuOpen={management.menuOpen}
          menuId={managementMenuId}
          {...(hideManage ? {} : {
            onManage: () => {
              if (management.menuOpen) management.closeMenu()
              else management.openMenu()
            },
          })}
        />
        {hideManage ? null : (
          <VPSManagementMenu
            lifecycleStatus={overview.identity.lifecycle_status}
            renewalDecision={overview.identity.renewal_decision}
            controller={management}
            menuId={managementMenuId}
            {...(managementTriggerRef ? { returnFocusRef: managementTriggerRef } : {})}
          />
        )}
      </div>

      <div className="vps-detail-route-nav">
        <SubjectLocalNavigation
          subject={subject}
          activeView="activity"
          overviewHref={basePath}
          overviewCurrent
        />
        <VPSDetailSectionNav />
      </div>

      {refreshError ? (
        <p className="create-form__error" role="status">
          本次刷新失败，当前仍展示上次成功数据。{refreshError}
        </p>
      ) : null}

      {overview.anomalies.length > 0 ? (
        <VPSOverviewAnomalies vpsId={vpsId} anomalies={overview.anomalies} onCommand={runCommand} />
      ) : null}

      <div className="vps-detail-workspace__lead">
        <section
          id="vps-section-identity"
          className="vps-detail-workspace__section vps-detail-workspace__region--facts"
          aria-labelledby="vps-section-identity-title"
        >
          <h2 id="vps-section-identity-title">资产信息</h2>
          <VPSOverviewFacts facts={overview.facts} identity={overview.identity} heading="" />
        </section>

        <section
          id="vps-section-ops"
          className="vps-detail-workspace__section vps-detail-workspace__region--billing"
          aria-labelledby="vps-section-ops-title"
        >
          <div className="vps-detail-workspace__section-head">
            <h2 id="vps-section-ops-title">订阅与续费</h2>
            <Link className="text-link" to={subscriptionsHref}>查看订阅列表</Link>
          </div>
          <VPSSubscriptionOpsBody
            decisionLabel={overviewSummaryCellLabel('renewal', overview.summary.renewal.status) || '—'}
            {...(primarySubscription ? { primary: primarySubscription } : {})}
            extras={resources.subscriptions.items.slice(1)}
            loading={resources.subscriptions.status === 'loading'}
            error={resources.subscriptions.status === 'error'
              ? (resources.subscriptions.error || '订阅暂不可用，请稍后重试。')
              : null}
            {...(resources.subscriptions.status === 'error'
              ? { onRetry: () => resources.retry('subscriptions') }
              : {})}
            empty={resources.subscriptions.status === 'ready'
              ? <p className="vps-detail-resource-group__empty">暂无订阅</p>
              : null}
            plannedCancellation={plannedCancellation}
            {...(plannedCancellation ? { cancellationPlanLabel: overviewLifecycleLabel('to_cancel') } : {})}
            trailing={(
              <VPSOverviewFreshness
                section={overview.summary.renewal.section}
                sourceLabel="续费"
                onRetry={onRefresh}
                retrying={retrying}
                omitReadyTime
              />
            )}
          />
        </section>
      </div>

      <section
        id="vps-section-monitoring"
        className="vps-detail-workspace__section"
        aria-labelledby="vps-section-monitoring-title"
      >
        <h2 id="vps-section-monitoring-title">运行观测</h2>
        <VPSOverviewSummaryGrid
          summary={overview.summary}
          keys={['overall', 'monitoring', 'ip_quality']}
          heading=""
          variant="metrics"
          onRefresh={onRefresh}
          retrying={retrying}
          lifecycleStatus={overview.identity.lifecycle_status}
          {...(monitoringRelations[0] ? { monitoringInstanceCount: monitoringRelations[0].count } : {})}
          after={{
            monitoring: (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => runCommand('open_monitoring_instances')}
              >
                {monitoringCount > 0 ? '查看实例' : '查看关联状态'}
              </Button>
            ),
            ip_quality: (
              <Link className="text-link" to={ipQualityHref} state={location.state}>
                {overviewIPQualityActionLabel(overview.summary.ip_quality)}
              </Link>
            ),
          }}
        />
      </section>

      <section
        id="vps-section-relations"
        className="vps-detail-workspace__section"
        aria-labelledby="vps-section-relations-title"
      >
        <div className="vps-detail-workspace__section-head">
          <h2 id="vps-section-relations-title">服务与域名</h2>
          <div className="vps-detail-workspace__section-actions">
            <Button type="button" size="sm" variant="secondary" onClick={() => runCommand('open_services')}>查看服务</Button>
            <Button type="button" size="sm" variant="secondary" onClick={() => runCommand('open_domains')}>查看域名</Button>
          </div>
        </div>
        <div className="vps-detail-workspace__resource-cols">
          <VPSDetailResourceList
            state={resources.services}
            groupLabel="服务"
            loadingLabel="正在加载服务…"
            emptyLabel="暂无服务"
            retryLabel="服务"
            getKey={(item) => item.service_id}
            getName={serviceResourceName}
            getType={serviceResourceType}
            getStatus={serviceResourceStatus}
            getAddress={(item) => item.url.trim()}
            getSummary={(item) => item.port != null ? `端口 ${item.port}` : ''}
            getCopyValue={(item) => item.url}
            getCopyLabel={() => '入口'}
            getHref={(item) => httpHref(item.url)}
            onRetry={() => resources.retry('services')}
            onOpenDetails={() => runCommand('open_services')}
          />
          <VPSDetailResourceList
            state={resources.domains}
            groupLabel="域名"
            loadingLabel="正在加载域名…"
            emptyLabel="暂无域名"
            retryLabel="域名"
            getKey={(item) => item.domain_id}
            getName={domainResourceName}
            getStatus={domainResourceStatus}
            getSummary={domainResourceExtra}
            onRetry={() => resources.retry('domains')}
            onOpenDetails={() => runCommand('open_domains')}
          />
        </div>
      </section>

      <section
        id="vps-section-activity"
        className="vps-detail-workspace__section"
        aria-labelledby="vps-overview-recent-title"
      >
        <VPSOverviewRecentActivity
          items={overview.recent_activity.items}
          activityHref={activityHref}
          section={overview.recent_activity.section}
          heading="最近活动"
          assetName={overview.identity.display_name}
          vpsId={vpsId}
          onRefresh={onRefresh}
          retrying={retrying}
        />
      </section>
    </div>
  )
}
