import type { MonitoringInstanceRecord } from '../../lib/types'

type MonitoringInstancesLabelsCellProps = {
  monitoringInstance: MonitoringInstanceRecord
}

export function MonitoringInstancesLabelsCell({ monitoringInstance }: MonitoringInstancesLabelsCellProps) {
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
