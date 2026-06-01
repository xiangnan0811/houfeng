import { Link } from 'react-router-dom'

import { MonoDigits, StatusGlyph } from '../../components/atoms'
import type { DashboardOverview } from '../../lib/types'
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
    <div className={`dashboard-overview-panel dashboard-overview-panel--${isMaintenance ? 'maintenance' : 'normal'}`}>
      <div className="dashboard-overview-panel__lead">
        <div className="dashboard-overview-panel__summary">
          <StatusGlyph state={isMaintenance ? 'maintenance' : 'normal'} size="md" />
          <div>
            <span className="dashboard-overview-panel__eyebrow">
              {isMaintenance ? '维护' : '运行'}
            </span>
            <h3>{isMaintenance ? '维护中' : '无活跃异常'}</h3>
            <p>
              {isMaintenance
                ? '维护态观察。'
                : '处理队列为空。'}
            </p>
          </div>
        </div>
        <Link
          className="dashboard-overview-panel__lead-link"
          to={isMaintenance ? DASHBOARD_LINKS.eventsMaintenance : DASHBOARD_LINKS.events24h}
          aria-label={isMaintenance ? '在工作台查看维护事件' : '在工作台查看 24h 事件流'}
        >
          {isMaintenance ? '查看维护事件' : '查看 24h 事件流'}
        </Link>
      </div>
      <div className="dashboard-overview-metrics" aria-label={isMaintenance ? '维护观察指标' : '运行概览指标'}>
        <Link className="dashboard-overview-metric" to={DASHBOARD_LINKS.monitoring}>
          <span>监控实例库存</span>
          <strong>
            <MonoDigits>{overview.total_monitoring_instance_count}</MonoDigits>
          </strong>
          <small>
            待接入 <MonoDigits>{overview.pending_onboarding_monitoring_instance_count}</MonoDigits> · 暂停{' '}
            <MonoDigits>{overview.paused_monitoring_instance_count}</MonoDigits>
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
      <div className="dashboard-overview-panel__support">
        <ManagementEntries overview={overview} />
        <DashboardContextStrip items={contextItems} />
      </div>
    </div>
  )
}
