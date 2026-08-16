import { MonoDigits, Timestamp } from '../../../../components/atoms'
import type { MonitoringEventEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: MonitoringEventEvidenceReadModel
}

export function MonitoringEventEvidenceRenderer({ model }: Props) {
  return (
    <section className="page-panel evidence-renderer evidence-renderer--events" aria-label="监控事件证据">
      <header className="evidence-renderer__header">
        <h3>监控事件</h3>
        <span>{model.quality_status} · <MonoDigits>{model.event_count}</MonoDigits> 条</span>
      </header>
      <ol className="evidence-renderer__timeline">
        {model.events.map((event) => (
          <li key={event.event_id}>
            <div>
              <strong>{event.summary}</strong>
              <span>{event.severity} · {event.event_type}{event.backfilled ? ' · 回填' : ''}</span>
            </div>
            <Timestamp value={event.event_at} />
          </li>
        ))}
      </ol>
    </section>
  )
}
