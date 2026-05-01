import { Hostname, StatusGlyph, Timestamp, type HealthState } from './atoms'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../lib/types'

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

type EventListProps = {
  events: StateChangeEventRecord[]
  emptyTitle?: string
  emptyDescription?: string
}

function incidentClassLabel(value: string) {
  return INCIDENT_CLASS_LABELS[value] ?? value
}

function eventTypeLabel(value: StateChangeEventRecord['event_type']) {
  return STATE_CHANGE_EVENT_TYPE_LABELS[value] ?? value
}

function objectTypeLabel(value: StateChangeEventRecord['object_type']) {
  return OBJECT_TYPE_LABELS[value] ?? value
}

function severityTone(value: StateChangeEventRecord['severity']) {
  if (value === '正常') return 'green'
  if (value === '关注') return 'yellow'
  if (value === '告警') return 'yellow'
  if (value === '严重') return 'red'
  return 'slate'
}

function severityGlyph(value: StateChangeEventRecord['severity']): HealthState {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  if (value === '严重') return 'critical'
  return 'offline'
}

function hasIncidentMeta(event: StateChangeEventRecord) {
  return event.incident_class.trim().length > 0
}

export function EventList({
  events,
  emptyTitle = '最近没有状态变更事件',
  emptyDescription = '系统暂时没有新的状态变更事件。',
}: EventListProps) {
  if (events.length === 0) {
    return (
      <div className="empty-state">
        <h3>{emptyTitle}</h3>
        <p>{emptyDescription}</p>
      </div>
    )
  }

  return (
    <div className="probe-list probe-list--timeline">
      {events.map((event) => (
        <article
          key={event.event_id ?? `${event.created_at}-${event.incident_id}-${event.event_type}`}
          className="probe-card probe-card--timeline"
        >
          <div className="probe-card__rail" aria-hidden>
            <StatusGlyph state={severityGlyph(event.severity)} size="sm" />
          </div>
          <div className="probe-card__body">
            <header className="probe-card__header">
              <div>
                <h3>{eventTypeLabel(event.event_type)}</h3>
                <p>{event.summary || '暂无摘要'}</p>
              </div>
              <div className="badge-row badge-row--wrap">
                {event.severity ? (
                  <StatusBadge label={event.severity} tone={severityTone(event.severity)} />
                ) : null}
                <StatusBadge label={objectTypeLabel(event.object_type)} tone="cyan" />
              </div>
            </header>
            <dl className="probe-card__meta">
              {hasIncidentMeta(event) ? (
                <div>
                  <dt>异常类型</dt>
                  <dd>{incidentClassLabel(event.incident_class)}</dd>
                </div>
              ) : null}
              <div>
                <dt>对象 ID</dt>
                <dd>
                  <Hostname>{event.object_id}</Hostname>
                </dd>
              </div>
              <div>
                <dt>发生时间</dt>
                <dd>
                  <Timestamp value={event.created_at} mode="absolute" />
                </dd>
              </div>
            </dl>
          </div>
        </article>
      ))}
    </div>
  )
}
