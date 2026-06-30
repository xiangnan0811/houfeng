import { Link } from 'react-router-dom'

import {
  type DataTableColumn,
  MonoDigits,
  StatusGlyph,
  Timestamp,
} from '../../components/atoms'
import type { MetricThresholds } from '../../config/thresholds'
import type { MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../../lib/types'
import {
  isBindingConflictMonitoringInstance,
  MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY,
  monitoringInstanceGlyphState,
} from './monitoringHelpers'
import { MonitoringInstancesLabelsCell } from './MonitoringInstancesLabelsCell'
import { MonitoringInstancesTrendCell } from './MonitoringInstancesTrendCell'

type BuildMonitoringInstancesTableColumnsArgs = {
  compareSet: Set<string>
  sparklines: MonitoringInstanceSparklinesResponse | null
  thresholds: MetricThresholds
  onToggleCompare: (monitoringInstanceId: string) => void
}

function issueSummary(monitoringInstance: MonitoringInstanceRecord): string {
  if (isBindingConflictMonitoringInstance(monitoringInstance)) return MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY
  if (monitoringInstance.current_primary_issue_summary.trim()) return monitoringInstance.current_primary_issue_summary
  if (!monitoringInstance.last_heartbeat_at) return '未收到心跳'
  return '心跳'
}

export function buildMonitoringInstancesTableColumns({
  compareSet,
  sparklines,
  thresholds,
  onToggleCompare,
}: BuildMonitoringInstancesTableColumnsArgs): DataTableColumn<MonitoringInstanceRecord>[] {
  return [
    {
      key: 'compare',
      label: '',
      width: 28,
      align: 'center',
      render: (monitoringInstance) => {
        const checked = compareSet.has(monitoringInstance.monitoring_instance_id)
        const disabled = !checked && compareSet.size >= 2
        return (
          <input
            type="checkbox"
            className="monitoring-table__compare-check"
            checked={checked}
            disabled={disabled}
            onChange={() => onToggleCompare(monitoringInstance.monitoring_instance_id)}
            onClick={(event) => event.stopPropagation()}
            aria-label={`选择 ${monitoringInstance.display_name} 进行对比`}
          />
        )
      },
    },
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (monitoringInstance) => (
        <StatusGlyph
          state={monitoringInstanceGlyphState(monitoringInstance)}
          size="md"
          ariaLabel={`${monitoringInstance.display_name} 健康 ${monitoringInstance.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '监控实例',
      width: 180,
      sortable: true,
      render: (monitoringInstance) => (
        <div className="monitoring-table__identity">
          <div className="monitoring-table__name-row">
            <Link
              className="text-link monitoring-table__name"
              to={`/monitoring/${monitoringInstance.monitoring_instance_id}`}
              onClick={(event) => event.stopPropagation()}
            >
              {monitoringInstance.display_name}
            </Link>
          </div>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置',
      width: 168,
      sortable: true,
      render: (monitoringInstance) => (
        <span className="monitoring-table__location">
          {[monitoringInstance.group, monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider].filter(Boolean).join(' · ') || '—'}
        </span>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      width: 132,
      render: (monitoringInstance) => (
        <MonitoringInstancesLabelsCell monitoringInstance={monitoringInstance} />
      ),
    },
    {
      key: 'issue',
      label: '当前主问题',
      width: 220,
      sortable: true,
      render: (monitoringInstance) => {
        const summary = issueSummary(monitoringInstance)
        return (
          <div className="monitoring-table__issue">
            <MonoDigits className="monitoring-table__issue-count">
              {monitoringInstance.current_active_incident_count}
            </MonoDigits>
            <span className="monitoring-table__issue-main">
              <span className="monitoring-table__issue-summary">{summary}</span>
              {monitoringInstance.last_heartbeat_at ? (
                <span className="monitoring-table__issue-heartbeat">
                  {summary === '心跳' ? null : '心跳 '}
                  <Timestamp value={monitoringInstance.last_heartbeat_at} mode="relative" />
                </span>
              ) : null}
            </span>
          </div>
        )
      },
    },
    {
      key: 'trends',
      label: '近 24h',
      width: 212,
      cellClassName: 'monitoring-table__trends',
      render: (monitoringInstance) => <MonitoringInstancesTrendCell monitoringInstance={monitoringInstance} sparklines={sparklines} thresholds={thresholds} />,
    },
  ]
}
