import { Card } from '../../components/atoms/Card'
import { MonoDigits, Timestamp } from '../../components/atoms'
import { Button } from '../../components/atoms/Button'
import { StatusBadge } from '../../components/StatusBadge'
import type { ActiveIncidentRecord, TargetRecord } from '../../lib/types'

type TargetDangerCardProps = {
  target: TargetRecord
  firstIncident: ActiveIncidentRecord | null
  onOpenEvents: () => void
}

export function TargetDangerCard({
  target,
  firstIncident,
  onOpenEvents,
}: TargetDangerCardProps) {
  return (
    <Card cardRole="warning" className="watchtower-danger" aria-label="当前主问题">
      <p className="watchtower-danger__eyebrow">当前主问题</p>
      <h2 className="watchtower-danger__summary">
        {target.current_primary_issue_summary || '存在活跃异常'}
      </h2>
      <p className="watchtower-danger__meta">
        共 <MonoDigits>{target.current_active_incident_count}</MonoDigits> 个活跃异常 · 健康状态{' '}
        <StatusBadge label={target.current_health_status} />
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
