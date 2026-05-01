import { Hostname, StatusGlyph, Timestamp, type HealthState } from './atoms'
import type { ActiveIncidentRecord } from '../lib/types'

import { StatusBadge } from './StatusBadge'

const INCIDENT_CLASS_LABELS: Record<string, string> = {
  node_heartbeat_missing: '节点心跳缺失',
  node_disk_pressure: '节点磁盘压力',
  node_inode_pressure: '节点 inode 压力',
  node_resource_pressure: '节点资源压力',
  target_probe_failure: '目标探测失败',
  target_tls_expiry: '目标 TLS 即将过期',
}

const OBJECT_TYPE_LABELS = {
  node: '节点',
  target: '目标',
} as const

const SEVERITY_ORDER = ['严重', '告警', '关注', '正常', ''] as const

type IncidentListProps = {
  incidents: ActiveIncidentRecord[]
  emptyTitle?: string
  emptyDescription?: string
}

function incidentClassLabel(value: string) {
  return INCIDENT_CLASS_LABELS[value] ?? value
}

function objectTypeLabel(value: ActiveIncidentRecord['object_type']) {
  return OBJECT_TYPE_LABELS[value] ?? value
}

function severityTone(value: ActiveIncidentRecord['severity']) {
  if (value === '正常') return 'green'
  if (value === '关注') return 'yellow'
  if (value === '告警') return 'yellow'
  return 'red'
}

function severityGlyph(value: ActiveIncidentRecord['severity']): HealthState {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  return 'critical'
}

function severityWeight(s: string): number {
  const idx = (SEVERITY_ORDER as readonly string[]).indexOf(s)
  return idx === -1 ? 999 : idx
}

export function IncidentList({
  incidents,
  emptyTitle = '当前没有活跃异常',
  emptyDescription = '一切正常，暂未发现需要处理的活跃异常。',
}: IncidentListProps) {
  if (incidents.length === 0) {
    return (
      <div className="empty-state">
        <h3>{emptyTitle}</h3>
        <p>{emptyDescription}</p>
      </div>
    )
  }

  // v2: severity-sorted dense rows. Active incidents are not chronological;
  // they are ranked by severity for "what to handle next" reading.
  const sorted = [...incidents].sort(
    (a, b) => severityWeight(a.severity) - severityWeight(b.severity),
  )

  return (
    <div className="probe-list probe-list--rows">
      {sorted.map((incident) => (
        <article key={incident.incident_id} className="probe-card probe-card--row">
          <div className="probe-card__lead">
            <StatusGlyph state={severityGlyph(incident.severity)} size="md" />
          </div>
          <div className="probe-card__body">
            <header className="probe-card__header">
              <div>
                <h3>{incidentClassLabel(incident.incident_class)}</h3>
                <p>{incident.source_summary || '暂无摘要'}</p>
              </div>
              <div className="badge-row badge-row--wrap">
                <StatusBadge label={incident.severity} tone={severityTone(incident.severity)} />
                <StatusBadge label={objectTypeLabel(incident.object_type)} tone="cyan" />
              </div>
            </header>
            <dl className="probe-card__meta">
              <div>
                <dt>对象 ID</dt>
                <dd>
                  <Hostname>{incident.object_id}</Hostname>
                </dd>
              </div>
              <div>
                <dt>开始时间</dt>
                <dd>
                  <Timestamp value={incident.started_at} mode="relative" />
                </dd>
              </div>
              <div>
                <dt>最近评估</dt>
                <dd>
                  <Timestamp value={incident.last_evaluated_at} mode="absolute" />
                </dd>
              </div>
            </dl>
          </div>
        </article>
      ))}
    </div>
  )
}
