import { Link } from 'react-router-dom'

import { MonoDigits, StatusGlyph } from '../../components/atoms'
import type { DashboardOverview } from '../../lib/types'
import { AssetDecisionSummary } from './AssetDecisionSummary'
import { DashboardContextStrip } from './DashboardContextStrip'
import { DASHBOARD_LINKS } from './dashboardLinks'
import { ManagementEntries } from './ManagementEntries'
import type { ContextItem } from './types'

type RunningOverviewProps = {
  overview: DashboardOverview
  maintenanceTotal: number
  contextItems: ContextItem[]
}

export function RunningOverview({ overview, maintenanceTotal, contextItems }: RunningOverviewProps) {
  const isMaintenance = maintenanceTotal > 0

  return (
    <div className="dashboard-overview-panel">
      <div className="dashboard-overview-panel__summary">
        <StatusGlyph state={isMaintenance ? 'maintenance' : 'normal'} size="md" />
        <div>
          <h3>{isMaintenance ? '维护观察中' : '当前没有活跃异常'}</h3>
          <p>
            {isMaintenance
              ? '维护对象进入观察状态，首页保留事件和库存上下文，不把维护态提升为紧急异常。'
              : '处理队列保持为空，首页转为运行概览与管理入口。'}
          </p>
        </div>
      </div>
      <div className="dashboard-overview-metrics" aria-label={isMaintenance ? '维护观察指标' : '运行概览指标'}>
        <Link className="dashboard-overview-metric" to={DASHBOARD_LINKS.nodes}>
          <span>节点库存</span>
          <strong>
            <MonoDigits>{overview.total_node_count}</MonoDigits>
          </strong>
          <small>
            待接入 <MonoDigits>{overview.pending_onboarding_node_count}</MonoDigits> · 暂停{' '}
            <MonoDigits>{overview.paused_node_count}</MonoDigits>
          </small>
        </Link>
        <Link className="dashboard-overview-metric" to={DASHBOARD_LINKS.targets}>
          <span>目标库存</span>
          <strong>
            <MonoDigits>{overview.total_target_count}</MonoDigits>
          </strong>
          <small>
            暂停 <MonoDigits>{overview.paused_target_count}</MonoDigits> · 归档{' '}
            <MonoDigits>{overview.archived_target_count}</MonoDigits>
          </small>
        </Link>
        <Link
          className="dashboard-overview-metric"
          to={isMaintenance ? DASHBOARD_LINKS.eventsMaintenance : DASHBOARD_LINKS.events24h}
        >
          <span>{isMaintenance ? '维护事件' : '24h 变化'}</span>
          <strong>
            <MonoDigits>
              {isMaintenance ? maintenanceTotal : overview.recent_new_incident_count + overview.recent_recovery_count}
            </MonoDigits>
          </strong>
          <small>
            新增 <MonoDigits>{overview.recent_new_incident_count}</MonoDigits> · 恢复{' '}
            <MonoDigits>{overview.recent_recovery_count}</MonoDigits>
          </small>
        </Link>
      </div>
      <AssetDecisionSummary summary={overview.asset_summary} />
      <ManagementEntries overview={overview} />
      <DashboardContextStrip items={contextItems} />
    </div>
  )
}
