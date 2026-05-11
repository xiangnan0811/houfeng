import { Link } from 'react-router-dom'

import type { DashboardOverview } from '../../lib/types'
import { DASHBOARD_LINKS } from './dashboardLinks'
import {
  eventManagementStat,
  nodeEntryLink,
  nodeManagementStat,
  notificationSummary,
  targetEntryLink,
  targetManagementStat,
} from './dashboardHelpers'
import type { ManagementEntry } from './types'

type ManagementEntriesProps = {
  overview: DashboardOverview
  showEventLink?: boolean
}

export function ManagementEntries({ overview, showEventLink = true }: ManagementEntriesProps) {
  const entries: ManagementEntry[] = [
    {
      title: '节点',
      stat: nodeManagementStat(overview),
      to: nodeEntryLink(overview),
    },
    {
      title: '目标',
      stat: targetManagementStat(overview),
      to: targetEntryLink(overview),
    },
    {
      title: '事件',
      stat: eventManagementStat(overview),
      to: DASHBOARD_LINKS.events24h,
    },
    {
      title: '设置',
      stat: notificationSummary(overview),
      to: DASHBOARD_LINKS.settings,
    },
  ]

  return (
    <div className="dashboard-management" aria-label="管理入口">
      <div className="dashboard-management__header">
        <h3>管理入口</h3>
        {showEventLink ? (
          <Link className="text-link" to={DASHBOARD_LINKS.events24h}>
            查看事件流
          </Link>
        ) : null}
      </div>
      <div className="dashboard-management__grid">
        {entries.map((entry) => (
          <Link
            className="dashboard-management-entry"
            to={entry.to}
            key={entry.title}
            aria-label={`${entry.title}：${entry.stat}`}
          >
            <span className="dashboard-management-entry__body">
              <span className="dashboard-management-entry__title">{entry.title}</span>
              <span className="dashboard-management-entry__stat">{entry.stat}</span>
            </span>
            <span className="dashboard-management-entry__cta">进入</span>
          </Link>
        ))}
      </div>
    </div>
  )
}
