import { Link } from 'react-router-dom'

import { DetailSection } from '../../components/DetailSection'
import type { DashboardOverview } from '../../lib/types'
import { AttentionQueue } from './AttentionQueue'
import { DashboardContextStrip } from './DashboardContextStrip'
import { DASHBOARD_LINKS } from './dashboardLinks'
import { buildContextItems } from './dashboardHelpers'
import { ManagementEntries } from './ManagementEntries'
import { OnboardingWorkbench } from './OnboardingWorkbench'
import { RunningOverview } from './RunningOverview'
import type { AttentionItem } from './types'

type DashboardWorkbenchProps = {
  overview: DashboardOverview
  attentionItems: AttentionItem[]
  abnormalTotal: number
  maintenanceTotal: number
  isFreshInstall: boolean
}

export function DashboardWorkbench({
  overview,
  attentionItems,
  abnormalTotal,
  maintenanceTotal,
  isFreshInstall,
}: DashboardWorkbenchProps) {
  const hasAbnormal = abnormalTotal > 0
  const isMaintenance = !hasAbnormal && maintenanceTotal > 0
  const title = isFreshInstall
    ? '首次接入工作台'
    : hasAbnormal
      ? '当前需要处理'
      : isMaintenance
        ? '维护观察'
        : '运行概览'
  const eyebrow = isFreshInstall
    ? '首次接入'
    : hasAbnormal
      ? undefined
      : isMaintenance
        ? '维护观察'
        : '运行概览'
  const ribbon: 'notice' | 'alert' | 'maintenance' | 'normal' = isFreshInstall
    ? 'notice'
    : hasAbnormal
      ? 'alert'
      : isMaintenance
        ? 'maintenance'
        : 'normal'
  const mode = isFreshInstall ? 'onboarding' : hasAbnormal ? 'abnormal' : isMaintenance ? 'maintenance' : 'normal'

  return (
    <DetailSection
      eyebrow={eyebrow}
      title={title}
      ribbon={ribbon}
      aside={
        hasAbnormal ? (
          <div className="dashboard-section-actions">
            <Link className="text-link" to={DASHBOARD_LINKS.monitoringAbnormal}>
              查看全部异常监控实例
            </Link>
            <Link className="text-link" to={DASHBOARD_LINKS.targetsAbnormal}>
              查看全部异常目标
            </Link>
            <Link className="text-link" to={DASHBOARD_LINKS.events24h}>
              查看事件流
            </Link>
          </div>
        ) : null
      }
    >
      <div className={`dashboard-workbench dashboard-workbench--${mode}`}>
        {isFreshInstall ? (
          <OnboardingWorkbench />
        ) : hasAbnormal ? (
          <div className="dashboard-incident-console">
            <div className="dashboard-incident-console__queue">
              <AttentionQueue items={attentionItems} />
            </div>
            <aside className="dashboard-incident-console__aside" aria-label="异常上下文">
              <DashboardContextStrip items={buildContextItems(overview, abnormalTotal, maintenanceTotal)} />
              <ManagementEntries overview={overview} showEventLink={false} />
            </aside>
          </div>
        ) : (
          <RunningOverview
            overview={overview}
            maintenanceTotal={maintenanceTotal}
            contextItems={buildContextItems(overview, abnormalTotal, maintenanceTotal)}
          />
        )}
      </div>
    </DetailSection>
  )
}
