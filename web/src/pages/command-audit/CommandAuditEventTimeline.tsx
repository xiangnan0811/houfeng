import { MonoDigits, Timestamp } from '../../components/atoms'
import type { CommandAuditEvent } from '../../lib/types'

const EVENT_LABELS: Record<CommandAuditEvent['event_type'], string> = {
  queued: '已排队',
  dispatched: '已派发',
  completed: '已完成',
  rejected: '已拒绝',
}

function compareEvents(left: CommandAuditEvent, right: CommandAuditEvent): number {
  const timeOrder = Date.parse(left.occurred_at) - Date.parse(right.occurred_at)
  return timeOrder === 0 ? left.audit_id.localeCompare(right.audit_id) : timeOrder
}

type CommandAuditEventTimelineProps = {
  actionID: string
  events: CommandAuditEvent[]
}

export function CommandAuditEventTimeline({ actionID, events }: CommandAuditEventTimelineProps) {
  const sortedEvents = [...events].sort(compareEvents)
  return (
    <dl
      className="metadata-list command-audit-timeline"
      role="region"
      aria-label={`${actionID} 原始审计事件`}
    >
      {sortedEvents.map((event) => (
        <div className="command-audit-timeline__event" key={event.audit_id}>
          <dt>
            <strong data-testid="command-audit-event-type">{EVENT_LABELS[event.event_type]}</strong>
            {' · '}
            <Timestamp value={event.occurred_at} mode="absolute" />
          </dt>
          <dd>
            <span>{event.source === 'web' ? 'Web' : 'Agent 同步'}</span>
            {event.exit_code != null ? <span> · 退出码 <MonoDigits>{event.exit_code}</MonoDigits></span> : null}
            {event.event_type === 'rejected'
              && event.rejection_reason === 'sensitive_confirmation_required'
              ? <span> · 缺少敏感命令确认</span>
              : null}
            <span> · </span>
            <MonoDigits className="command-audit-timeline__id">{event.audit_id}</MonoDigits>
          </dd>
        </div>
      ))}
    </dl>
  )
}
