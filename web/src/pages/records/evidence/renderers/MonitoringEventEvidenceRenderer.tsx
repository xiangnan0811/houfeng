import { useId } from 'react'

import { Badge, type BadgeTone, Timestamp } from '../../../../components/atoms'
import { eventTypeLabel, toneOf } from '../evidencePresentation'
import type { MonitoringEventEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: MonitoringEventEvidenceReadModel
}

const SEVERITY_TONES: Record<string, BadgeTone> = {
  正常: 'normal',
  关注: 'notice',
  告警: 'alert',
  严重: 'critical',
}

export function MonitoringEventEvidenceRenderer({ model }: Props) {
  const titleId = useId()
  return (
    <section className="record-section record-evidence__body" aria-labelledby={titleId}>
      <div className="record-section__head">
        <h2 className="record-section__title" id={titleId}>
          监控事件 <span className="record-count">{model.event_count}</span>
        </h2>
      </div>
      <ol className="record-evidence__timeline">
        {model.events.map((event) => (
          <li key={event.event_id} className="record-evidence__event">
            <Timestamp value={event.event_at} className="record-evidence__event-time" />
            <div className="record-evidence__event-body">
              <strong>{event.summary || eventTypeLabel(event.event_type)}</strong>
              <span className="record-evidence__event-meta">
                {event.severity ? (
                  // 恢复事件的严重程度描述的是恢复前状态，不再用告警色强调。
                  <Badge variant="info" tone={event.event_type === 'incident_recovered' ? 'neutral' : toneOf(SEVERITY_TONES, event.severity)}>
                    {event.severity}
                  </Badge>
                ) : null}
                <span>{eventTypeLabel(event.event_type)}</span>
                {event.backfilled ? <span className="record-evidence__tag">回填</span> : null}
              </span>
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}
