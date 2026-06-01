import { Link } from 'react-router-dom'

import { StatusBadge } from '../../components/StatusBadge'
import {
  type DataTableColumn,
  MonoDigits,
  StatusGlyph,
  Timestamp,
  Badge,
} from '../../components/atoms'
import type { AssetContextForMonitoringInstance, MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../../lib/types'
import {
  assetContextHasAttention,
  assetContextMessage,
  assetContextPrimarySummary,
  subscriptionStateLabel,
  vpsLifecycleLabel,
} from '../assetContextSummary'
import {
  isBindingConflictMonitoringInstance,
  MONITORING_INSTANCE_BINDING_CONFLICT_STATUS,
  MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY,
  monitoringInstanceGlyphState,
} from './monitoringHelpers'
import { MonitoringInstancesActionsCell } from './MonitoringInstancesActionsCell'
import { MonitoringInstancesLabelsCell } from './MonitoringInstancesLabelsCell'
import { MonitoringInstancesTrendCell } from './MonitoringInstancesTrendCell'
import type { MonitoringInstanceRuntimeAction } from './types'

type BuildMonitoringInstancesTableColumnsArgs = {
  compareSet: Set<string>
  sparklines: MonitoringInstanceSparklinesResponse | null
  assetContexts: Map<string, AssetContextForMonitoringInstance>
  editingLabelMonitoringInstanceId: string | null
  labelDraft: string
  groupDraft: string
  metadataBusyMonitoringInstanceId: string | null
  metadataErrors: Record<string, string>
  runtimeBusyMonitoringInstanceId: string | null
  actionButtonRefs: { current: Record<string, HTMLButtonElement | null> }
  onToggleCompare: (monitoringInstanceId: string) => void
  onLabelDraftChange: (value: string) => void
  onGroupDraftChange: (value: string) => void
  onSaveLabels: (monitoringInstance: MonitoringInstanceRecord) => void
  onCancelLabels: (monitoringInstance: MonitoringInstanceRecord) => void
  onStartLabelEdit: (monitoringInstance: MonitoringInstanceRecord) => void
  onRuntimeAction: (monitoringInstance: MonitoringInstanceRecord, action: MonitoringInstanceRuntimeAction) => void
  onQueueFocusRestore: (monitoringInstanceId: string, action: MonitoringInstanceRuntimeAction) => void
}

export function buildMonitoringInstancesTableColumns({
  compareSet,
  sparklines,
  assetContexts,
  editingLabelMonitoringInstanceId,
  labelDraft,
  groupDraft,
  metadataBusyMonitoringInstanceId,
  metadataErrors,
  runtimeBusyMonitoringInstanceId,
  actionButtonRefs,
  onToggleCompare,
  onLabelDraftChange,
  onGroupDraftChange,
  onSaveLabels,
  onCancelLabels,
  onStartLabelEdit,
  onRuntimeAction,
  onQueueFocusRestore,
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
            {monitoringInstance.monitoring_status !== '启用' ? (
              <Badge tone={monitoringInstance.monitoring_status === '维护中' ? 'maintenance' : 'offline'}>{monitoringInstance.monitoring_status}</Badge>
            ) : null}
            {monitoringInstance.lifecycle_status !== '在用' && monitoringInstance.lifecycle_status !== '待接入' ? (
              <Badge tone={monitoringInstance.lifecycle_status === '已退役' ? 'offline' : 'notice'}>{monitoringInstance.lifecycle_status}</Badge>
            ) : null}
          </div>
          <span className="monitoring-table__freshness">
            心跳 <Timestamp value={monitoringInstance.last_heartbeat_at} mode="relative" />
            {monitoringInstance.last_sync_at ? <> · 同步 <Timestamp value={monitoringInstance.last_sync_at} mode="relative" /></> : null}
          </span>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置',
      sortable: true,
      render: (monitoringInstance) => (
        <span className="monitoring-table__location">
          {[monitoringInstance.group, monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider].filter(Boolean).join(' · ') || '—'}
        </span>
      ),
    },
    {
      key: 'asset_context',
      label: '资产上下文',
      render: (monitoringInstance) => {
        const context = assetContexts.get(monitoringInstance.monitoring_instance_id)
        const primary = assetContextPrimarySummary(context)
        if (!context || !primary) {
          return <span className="asset-context-pill">未关联 VPS</span>
        }
        return (
          <div className="asset-context-cell">
            <span className={assetContextHasAttention(context) ? 'asset-context-pill asset-context-pill--attention' : 'asset-context-pill'}>
              {assetContextMessage(context)}
            </span>
            <small>
              {vpsLifecycleLabel(primary.lifecycle_status)} · {subscriptionStateLabel(primary.subscription_state)}
            </small>
          </div>
        )
      },
    },
    {
      key: 'labels',
      label: '标签',
      render: (monitoringInstance) => (
        <MonitoringInstancesLabelsCell
          monitoringInstance={monitoringInstance}
          editing={editingLabelMonitoringInstanceId === monitoringInstance.monitoring_instance_id}
          labelDraft={labelDraft}
          groupDraft={groupDraft}
          metadataBusyMonitoringInstanceId={metadataBusyMonitoringInstanceId}
          metadataError={metadataErrors[monitoringInstance.monitoring_instance_id]}
          onLabelDraftChange={onLabelDraftChange}
          onGroupDraftChange={onGroupDraftChange}
          onSaveLabels={onSaveLabels}
          onCancelLabels={onCancelLabels}
        />
      ),
    },
    {
      key: 'issue',
      label: '当前主问题',
      sortable: true,
      render: (monitoringInstance) => {
        const summary = isBindingConflictMonitoringInstance(monitoringInstance)
          ? MONITORING_INSTANCE_BINDING_CONFLICT_SUMMARY
          : monitoringInstance.current_primary_issue_summary || '暂无明显异常'
        return (
          <div className="monitoring-table__issue">
            <MonoDigits className="monitoring-table__issue-count">
              {monitoringInstance.current_active_incident_count}
            </MonoDigits>
            <span className="monitoring-table__issue-summary">{summary}</span>
            {isBindingConflictMonitoringInstance(monitoringInstance) ? (
              <StatusBadge label={MONITORING_INSTANCE_BINDING_CONFLICT_STATUS} />
            ) : null}
          </div>
        )
      },
    },
    {
      key: 'trends',
      label: '近 24h',
      cellClassName: 'monitoring-table__trends',
      render: (monitoringInstance) => <MonitoringInstancesTrendCell monitoringInstance={monitoringInstance} sparklines={sparklines} />,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (monitoringInstance) => (
        <MonitoringInstancesActionsCell
          monitoringInstance={monitoringInstance}
          editingLabelMonitoringInstanceId={editingLabelMonitoringInstanceId}
          metadataBusyMonitoringInstanceId={metadataBusyMonitoringInstanceId}
          runtimeBusyMonitoringInstanceId={runtimeBusyMonitoringInstanceId}
          actionButtonRefs={actionButtonRefs}
          onStartLabelEdit={onStartLabelEdit}
          onRuntimeAction={onRuntimeAction}
          onQueueFocusRestore={onQueueFocusRestore}
        />
      ),
    },
  ]
}
