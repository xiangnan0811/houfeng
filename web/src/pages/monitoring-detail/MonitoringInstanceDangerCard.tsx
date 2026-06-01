import { Card } from '../../components/atoms/Card'
import { MonoDigits, Timestamp } from '../../components/atoms'
import { Button } from '../../components/atoms/Button'
import { StatusBadge } from '../../components/StatusBadge'
import type { ActiveIncidentRecord, MonitoringInstanceRecord } from '../../lib/types'

type MonitoringInstanceDangerCardProps = {
  monitoringInstance: MonitoringInstanceRecord
  firstIncident: ActiveIncidentRecord | null
  onOpenEvents: () => void
}

export function MonitoringInstanceDangerCard({ monitoringInstance, firstIncident, onOpenEvents }: MonitoringInstanceDangerCardProps) {
  return (
    <Card cardRole="warning" className="watchtower-danger" aria-label="当前主问题">
      <p className="watchtower-danger__eyebrow">当前主问题</p>
      <h2 className="watchtower-danger__summary">
        {monitoringInstance.current_primary_issue_summary || '存在活跃异常'}
      </h2>
      <p className="watchtower-danger__meta">
        活跃异常 <MonoDigits>{monitoringInstance.current_active_incident_count}</MonoDigits> 个 · 健康状态{' '}
        <StatusBadge label={monitoringInstance.current_health_status} />
        {firstIncident?.started_at ? (
          <> · 持续 <Timestamp value={firstIncident.started_at} mode="relative" /></>
        ) : null}
      </p>
      <div className="watchtower-danger__actions">
        <Button variant="ghost" size="sm" onClick={onOpenEvents}>
          查看完整时间线 →
        </Button>
      </div>
    </Card>
  )
}
