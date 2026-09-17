import { Link } from 'react-router-dom'

import {
  type DataTableColumn,
  Badge,
  Hostname,
  MonoDigits,
  Timestamp,
} from '../../components/atoms'
import type { MetricThresholds } from '../../config/thresholds'
import type {
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeSummariesResponse,
  MonitoringInstanceSparklinesResponse,
} from '../../lib/types'
import { heartbeatFreshnessLabel } from './heartbeatFreshness'
import {
  isBindingConflictMonitoringInstance,
  MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY,
  monitoringInstanceHealthLabel,
  monitoringInstanceHealthTone,
  formatNetworkRate,
  formatSampledUptime,
  monitoringIssueSummary,
} from './monitoringHelpers'
import { MonitoringInstancesTrendCell } from './MonitoringInstancesTrendCell'
import type { HeartbeatFreshness } from './types'

type BuildMonitoringInstancesTableColumnsArgs = {
  selectedIds: string[]
  allVisibleSelected: boolean
  someVisibleSelected: boolean
  sparklines: MonitoringInstanceSparklinesResponse | null
  thresholds: MetricThresholds | null
  detailState: object
  freshnessById: Map<string, HeartbeatFreshness>
  snapshotReadAt: Date | null
  navigationLocked: boolean
  onToggleSelected: (monitoringInstanceId: string) => void
  onToggleSelectAll: (checked: boolean) => void
  runtimeSummaries?: MonitoringInstanceRuntimeSummariesResponse | null
}

export function buildMonitoringInstancesTableColumns({
  selectedIds,
  allVisibleSelected,
  someVisibleSelected,
  sparklines,
  thresholds,
  detailState,
  freshnessById,
  snapshotReadAt,
  navigationLocked,
  onToggleSelected,
  onToggleSelectAll,
  runtimeSummaries,
}: BuildMonitoringInstancesTableColumnsArgs): DataTableColumn<MonitoringInstanceRecord>[] {
  const selectedSet = new Set(selectedIds)
  const summaryReadAt = (() => {
    if (!runtimeSummaries?.read_at) return null
    const parsed = new Date(runtimeSummaries.read_at)
    return Number.isNaN(parsed.getTime()) ? null : parsed
  })()
  return [
    {
      key: 'select',
      label: (
        <input
          type="checkbox"
          className="monitoring-table__select-check"
          checked={allVisibleSelected}
          disabled={navigationLocked}
          ref={(node) => {
            if (node) node.indeterminate = someVisibleSelected
          }}
          onChange={(event) => onToggleSelectAll(event.target.checked)}
          aria-label="全选可见监控实例"
        />
      ),
      width: 40,
      align: 'center',
      cellClassName: 'monitoring-table__select',
      render: (monitoringInstance) => {
        if (monitoringInstance.archived_at) return null
        return (
          <input
            type="checkbox"
            className="monitoring-table__select-check"
            checked={selectedSet.has(monitoringInstance.monitoring_instance_id)}
            disabled={navigationLocked}
            onChange={() => onToggleSelected(monitoringInstance.monitoring_instance_id)}
            onClick={(event) => event.stopPropagation()}
            aria-label={`选择 ${monitoringInstance.display_name}`}
          />
        )
      },
    },
    {
      key: 'identity',
      label: '监控实例',
      width: 180,
      sortable: true,
      render: (monitoringInstance) => (
        <Link
          className="text-link monitoring-table__name"
          to={`/monitoring/${monitoringInstance.monitoring_instance_id}`}
          state={detailState}
          title={monitoringInstance.display_name}
          onClick={(event) => event.stopPropagation()}
        >
          {monitoringInstance.display_name}
        </Link>
      ),
    },
    {
      key: 'health',
      label: '健康',
      width: 150,
      sortable: true,
      sortKey: 'health',
      render: (monitoringInstance) => {
        const freshness = freshnessById.get(monitoringInstance.monitoring_instance_id)
        const hasHeartbeat = freshness != null && freshness.kind !== 'missing' && freshness.kind !== 'invalid'
        const healthLabel = monitoringInstanceHealthLabel(monitoringInstance, hasHeartbeat)
        const summary = monitoringIssueSummary(monitoringInstance)
        const incidentCount = monitoringInstance.current_active_incident_count
        return (
          <div className="monitoring-table__health">
            <span className="monitoring-table__health-head">
              <Badge variant="state" tone={monitoringInstanceHealthTone(monitoringInstance, hasHeartbeat)}>
                {healthLabel}
              </Badge>
              {incidentCount > 0 ? (
                <MonoDigits className="monitoring-table__issue-count">{incidentCount}</MonoDigits>
              ) : null}
            </span>
            {summary ? <span className="monitoring-table__issue-summary" title={summary}>{summary}</span> : null}
          </div>
        )
      },
    },
    {
      key: 'heartbeat',
      label: '心跳',
      width: 160,
      sortable: true,
      render: (monitoringInstance) => {
        const freshness = freshnessById.get(monitoringInstance.monitoring_instance_id) ?? { kind: 'missing' as const }
        return (
          <div className="monitoring-table__heartbeat">
            {renderHeartbeatEvidence(freshness, snapshotReadAt)}
            <span className="monitoring-table__runtime-flags">
              {freshness.kind === 'stale' ? (
                <Badge variant="state" tone="notice">{heartbeatFreshnessLabel(freshness)}</Badge>
              ) : null}
              {freshness.kind === 'policy-unavailable' ? (
                <Badge variant="state" tone="notice">{heartbeatFreshnessLabel(freshness)}</Badge>
              ) : null}
              {monitoringInstance.monitoring_status === '暂停' ? (
                <Badge variant="state" tone="offline">暂停</Badge>
              ) : null}
              {monitoringInstance.monitoring_status === '维护中' ? (
                <Badge variant="state" tone="maintenance">维护中</Badge>
              ) : null}
              {monitoringInstance.binding_status === '未绑定' ? (
                <Badge variant="state" tone="offline">未绑定</Badge>
              ) : null}
              {isBindingConflictMonitoringInstance(monitoringInstance) ? (
                <Badge variant="state" tone="notice">{MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY}</Badge>
              ) : null}
            </span>
          </div>
        )
      },
    },
    {
      key: 'uptime',
      label: '运行时长',
      width: 100,
      render: (monitoringInstance) => {
        const summary = runtimeSummaries?.monitoring_instances?.[monitoringInstance.monitoring_instance_id]
        if (!summary) {
          return <span className="monitoring-table__empty">—</span>
        }
        const formatted = formatSampledUptime(summary.uptime_seconds)
        if (formatted === '—') {
          return <span className="monitoring-table__empty">—</span>
        }
        return (
          <div
            className="monitoring-table__uptime"
            title={summary.observed_at ? `采样时间：${summary.observed_at}` : undefined}
          >
            <span className="monitoring-table__uptime-value">
              <MonoDigits>{formatted}</MonoDigits>
            </span>
            {summary.observed_at ? (
              <span className="monitoring-table__uptime-sample">
                采样{' '}
                {summaryReadAt ? (
                  <Timestamp
                    value={summary.observed_at}
                    mode="relative"
                    now={summaryReadAt}
                  />
                ) : (
                  <span className="mono tnum timestamp">—</span>
                )}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      key: 'network',
      label: '网络速率',
      width: 140,
      render: (monitoringInstance) => {
        const summary = runtimeSummaries?.monitoring_instances?.[monitoringInstance.monitoring_instance_id]
        const outRate = summary ? formatNetworkRate(summary.net_out_bytes_per_sec) : '—'
        const inRate = summary ? formatNetworkRate(summary.net_in_bytes_per_sec) : '—'
        return (
          <div
            className="monitoring-table__network"
            title={summary?.observed_at ? `采样时间：${summary.observed_at}` : undefined}
          >
            <div
              className="monitoring-table__network-line"
              title={`出站速率 (上行)：${outRate}`}
            >
              <span className="monitoring-table__network-direction" aria-label="上行">↑</span>
              <MonoDigits className="monitoring-table__network-rate">{outRate}</MonoDigits>
            </div>
            <div
              className="monitoring-table__network-line"
              title={`入站速率 (下行)：${inRate}`}
            >
              <span className="monitoring-table__network-direction" aria-label="下行">↓</span>
              <MonoDigits className="monitoring-table__network-rate">{inRate}</MonoDigits>
            </div>
          </div>
        )
      },
    },
    {
      key: 'trends',
      label: '24h 资源趋势',
      cellClassName: 'monitoring-table__trends',
      render: (monitoringInstance) => (
        <MonitoringInstancesTrendCell
          monitoringInstance={monitoringInstance}
          sparklines={sparklines}
          thresholds={thresholds}
        />
      ),
    },
  ]
}


function renderHeartbeatEvidence(freshness: HeartbeatFreshness, now: Date | null) {
  if (freshness.kind === 'missing') {
    return <span className="monitoring-table__heartbeat-label">{heartbeatFreshnessLabel(freshness)}</span>
  }
  if (freshness.kind === 'invalid') {
    return (
      <>
        <span className="monitoring-table__heartbeat-label">{heartbeatFreshnessLabel(freshness)}</span>
        <Hostname className="monitoring-table__heartbeat-raw">{freshness.raw}</Hostname>
      </>
    )
  }
  return (
    <span className="monitoring-table__heartbeat-time">
      <Timestamp value={freshness.at} mode="relative" {...(now ? { now } : {})} />
    </span>
  )
}
