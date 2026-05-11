import { StatusBadge } from '../../components/StatusBadge'
import { MonoDigits, StatusGlyph, Timestamp } from '../../components/atoms'
import { Button } from '../../components/atoms/Button'
import type { ActiveIncidentRecord, HostSample, NodeRecord } from '../../lib/types'
import { nodeHealthGlyphState } from './nodeDetailHelpers'

type NodeDiagnosisSummaryProps = {
  node: NodeRecord
  sample: HostSample | null
  incidents: ActiveIncidentRecord[]
  eventsError: string | null
  onOpenEvents: () => void
  onOpenIncidents: () => void
}

export function NodeDiagnosisSummary({
  node,
  sample,
  incidents,
  eventsError,
  onOpenEvents,
  onOpenIncidents,
}: NodeDiagnosisSummaryProps) {
  const hasIncident = node.current_active_incident_count > 0
  const containerCount = sample?.containers?.length ?? 0

  return (
    <section
      className={`watchtower-diagnosis watchtower-diagnosis--${nodeHealthGlyphState(node)}`}
      aria-label="节点诊断摘要"
    >
      <div className="watchtower-diagnosis__lead">
        <StatusGlyph
          state={nodeHealthGlyphState(node)}
          size="md"
          ariaLabel={`${node.display_name} 健康 ${node.current_health_status}`}
        />
        <div>
          <p className="watchtower-diagnosis__eyebrow">诊断摘要</p>
          <strong className="watchtower-diagnosis__title">
            {hasIncident
              ? node.current_primary_issue_summary || '存在活跃异常'
              : sample
                ? '节点当前没有活跃异常'
                : '等待首批主机样本'}
          </strong>
          <p>
            健康 <StatusBadge label={node.current_health_status} /> · 接入 <StatusBadge label={node.binding_status} /> · 运行{' '}
            <StatusBadge label={node.monitoring_status} />
          </p>
        </div>
      </div>
      <div className="watchtower-diagnosis__facts" aria-label="关键运行事实">
        <div>
          <span>最后心跳</span>
          <strong>
            <Timestamp value={node.last_heartbeat_at} mode="relative" />
          </strong>
        </div>
        <div>
          <span>活跃异常</span>
          <strong>
            <MonoDigits>{node.current_active_incident_count}</MonoDigits>
          </strong>
        </div>
        <div>
          <span>历史线索</span>
          <strong>
            <MonoDigits>{incidents.length}</MonoDigits>
          </strong>
        </div>
        <div>
          <span>容器</span>
          <strong>
            <MonoDigits>{containerCount}</MonoDigits>
          </strong>
        </div>
      </div>
      <div className="watchtower-diagnosis__actions">
        <Button variant={hasIncident ? 'primary' : 'secondary'} size="sm" onClick={onOpenEvents}>
          查看事件时间线
        </Button>
        <Button variant="ghost" size="sm" onClick={onOpenIncidents}>
          查看历史异常
        </Button>
        {eventsError ? <span role="alert">事件暂不可用：{eventsError}</span> : null}
      </div>
    </section>
  )
}
