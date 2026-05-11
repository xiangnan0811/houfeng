import { Link } from 'react-router-dom'

import { MonoDigits, StatusGlyph, Timestamp } from '../../components/atoms'
import {
  AUTO_REFRESH_OPTIONS,
  type AutoRefreshOption,
} from '../../lib/useAutoRefresh'
import type { DashboardOverview } from '../../lib/types'
import { fleetGlyphState, fleetSignalLabel } from './dashboardHelpers'
import { DashboardTrendPulse } from './DashboardTrendPulse'
import type { DashboardMetric, FleetState } from './types'

type FleetStatePanelProps = {
  overview: DashboardOverview
  fleetState: FleetState
  metrics: DashboardMetric[]
  refreshing?: boolean
  onRefresh?: () => void
  autoRefresh?: AutoRefreshOption
  onAutoRefreshChange?: (value: AutoRefreshOption) => void
}

export function FleetStatePanel({
  overview,
  fleetState,
  metrics,
  refreshing = false,
  onRefresh,
  autoRefresh,
  onAutoRefreshChange,
}: FleetStatePanelProps) {
  return (
    <section
      className={`dashboard-status-bar dashboard-status-bar--${fleetState.tone}`}
      aria-label="Dashboard 状态"
    >
      <div className="dashboard-status-bar__body">
        <div className="dashboard-status-bar__headline">
          <div
            className={`dashboard-status-bar__signal dashboard-status-bar__signal--${fleetState.tone}`}
            aria-hidden="true"
          >
            <StatusGlyph state={fleetGlyphState(fleetState.tone)} size="md" />
            <span>{fleetSignalLabel(fleetState.tone)}</span>
          </div>
          <p className="dashboard-status-bar__eyebrow">全局状态</p>
        </div>
        <h1 className="dashboard-status-bar__title">{fleetState.title}</h1>
        <p className="dashboard-status-bar__description">{fleetState.description}</p>
        <span className="dashboard-status-bar__generated">
          摘要生成 <Timestamp value={overview.snapshot_generated_at} mode="absolute" />
        </span>
      </div>
      {metrics.length > 0 ? (
        <div className="dashboard-status-bar__telemetry">
          <DashboardTrendPulse overview={overview} tone={fleetState.tone} />
          <div className="dashboard-status-bar__metrics" aria-label="关键状态指标">
            {metrics.map((metric) => (
              <Link
                className={`dashboard-inline-metric${metric.tone ? ` dashboard-inline-metric--${metric.tone}` : ''}`}
                to={metric.to}
                key={metric.label}
                aria-label={`${metric.label}：${metric.detail}`}
              >
                <span className="dashboard-inline-metric__label">{metric.label}</span>
                <strong className="dashboard-inline-metric__value">
                  <MonoDigits>{metric.value}</MonoDigits>
                </strong>
                <span className="dashboard-inline-metric__detail">{metric.detail}</span>
              </Link>
            ))}
          </div>
        </div>
      ) : null}
      <div className="dashboard-status-bar__actions" aria-label="首页主要入口">
        <div className="dashboard-status-bar__cta-row">
          <Link className="btn btn--primary btn--md" to={fleetState.primaryCta.to}>
            {fleetState.primaryCta.label}
          </Link>
          {fleetState.secondaryCtas.map((cta) => (
            <Link
              className="btn btn--ghost btn--md"
              to={cta.to}
              key={cta.label}
            >
              {cta.label}
            </Link>
          ))}
        </div>
        <div className="dashboard-status-bar__utility-row">
          {onRefresh ? (
            <button
              type="button"
              className="btn btn--ghost btn--md"
              disabled={refreshing}
              onClick={onRefresh}
            >
              {refreshing ? '刷新中…' : '刷新'}
            </button>
          ) : null}
          {onAutoRefreshChange ? (
            <label className="dashboard-status-bar__refresh">
              <span>自动刷新</span>
              <select
                className="auto-refresh-select"
                value={autoRefresh == null ? '' : String(autoRefresh)}
                onChange={(e) => {
                  const v = e.target.value
                  onAutoRefreshChange(v === '' ? null : Number(v))
                }}
                aria-label="自动刷新间隔"
              >
                {AUTO_REFRESH_OPTIONS.map((opt) => (
                  <option key={opt.label} value={opt.value == null ? '' : String(opt.value)}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
        </div>
      </div>
    </section>
  )
}
