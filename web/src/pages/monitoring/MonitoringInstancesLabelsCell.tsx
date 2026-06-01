import { Button } from '../../components/atoms'
import type { MonitoringInstanceRecord } from '../../lib/types'

type MonitoringInstancesLabelsCellProps = {
  monitoringInstance: MonitoringInstanceRecord
  editing: boolean
  labelDraft: string
  groupDraft: string
  metadataBusyMonitoringInstanceId: string | null
  metadataError?: string
  onLabelDraftChange: (value: string) => void
  onGroupDraftChange: (value: string) => void
  onSaveLabels: (monitoringInstance: MonitoringInstanceRecord) => void
  onCancelLabels: (monitoringInstance: MonitoringInstanceRecord) => void
}

function renderLabelsCell(monitoringInstance: MonitoringInstanceRecord) {
  if (monitoringInstance.labels.length === 0) return <span className="empty-inline">—</span>
  const visible = monitoringInstance.labels.slice(0, 3)
  const overflow = monitoringInstance.labels.length - visible.length
  return (
    <span className="monitoring-table__labels">
      {visible.join(' · ')}
      {overflow > 0 ? (
        <span className="monitoring-table__labels-more"> +{overflow}</span>
      ) : null}
    </span>
  )
}

export function MonitoringInstancesLabelsCell({
  monitoringInstance,
  editing,
  labelDraft,
  groupDraft,
  metadataBusyMonitoringInstanceId,
  metadataError,
  onLabelDraftChange,
  onGroupDraftChange,
  onSaveLabels,
  onCancelLabels,
}: MonitoringInstancesLabelsCellProps) {
  if (!editing) return renderLabelsCell(monitoringInstance)

  return (
    <div
      className="monitoring-table__label-editor"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.stopPropagation()
        }
      }}
    >
      <label className="monitoring-table__label-editor-field">
        <span className="visually-hidden">Group</span>
        <input
          name={`group-${monitoringInstance.monitoring_instance_id}`}
          value={groupDraft}
          onChange={(event) => onGroupDraftChange(event.target.value)}
          aria-label="Group"
          placeholder="Group"
        />
      </label>
      <label className="monitoring-table__label-editor-field">
        <span className="visually-hidden">标签</span>
        <input
          name={`labels-${monitoringInstance.monitoring_instance_id}`}
          value={labelDraft}
          onChange={(event) => onLabelDraftChange(event.target.value)}
          aria-label="标签"
        />
      </label>
      <div className="monitoring-table__label-editor-actions">
        <Button
          size="sm"
          variant="primary"
          disabled={metadataBusyMonitoringInstanceId === monitoringInstance.monitoring_instance_id}
          onClick={() => onSaveLabels(monitoringInstance)}
        >
          {metadataBusyMonitoringInstanceId === monitoringInstance.monitoring_instance_id ? '正在保存…' : '保存标签'}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={metadataBusyMonitoringInstanceId === monitoringInstance.monitoring_instance_id}
          onClick={() => onCancelLabels(monitoringInstance)}
        >
          取消
        </Button>
      </div>
      {metadataError ? (
        <p className="monitoring-table__inline-error" role="alert">
          {metadataError}
        </p>
      ) : null}
    </div>
  )
}
