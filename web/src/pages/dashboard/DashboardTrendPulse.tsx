import { Link } from 'react-router-dom'

import { Sparkline } from '../../components/atoms'
import type { DashboardOverview } from '../../lib/types'
import { DASHBOARD_LINKS } from './dashboardLinks'
import { trendBalanceLabel, trendValues } from './dashboardHelpers'
import type { FleetStateTone } from './types'

type DashboardTrendPulseProps = {
  overview: DashboardOverview
  tone: FleetStateTone
}

export function DashboardTrendPulse({ overview, tone }: DashboardTrendPulseProps) {
  const values = trendValues(overview)
  const changeLabel = `新增 ${overview.recent_new_incident_count} · 恢复 ${overview.recent_recovery_count}`

  return (
    <Link
      className={`dashboard-trend-pulse dashboard-trend-pulse--${tone}`}
      to={DASHBOARD_LINKS.events24h}
      aria-label={`24h 事件趋势：${changeLabel}`}
    >
      <span className="dashboard-trend-pulse__copy">
        <span className="dashboard-trend-pulse__label">24h 事件趋势</span>
        <strong>{trendBalanceLabel(overview)}</strong>
        <span>{changeLabel}</span>
      </span>
      {values.length > 0 ? (
        <span className="dashboard-trend-pulse__chart">
          <Sparkline
            values={values}
            tone={tone}
            width={96}
            height={24}
            expand
            ariaLabel="24h 新增异常趋势"
            formatValue={(value) => `${Math.round(value)} 次`}
          />
        </span>
      ) : null}
    </Link>
  )
}
