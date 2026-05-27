import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../../lib/types'

type EventGroupKey = 'today' | 'yesterday' | 'this_week' | 'earlier'

type EventsStreamSectionProps = {
  events: StateChangeEventRecord[]
  exhausted: boolean
  loadingMore: boolean
  hasActiveFilters: boolean
  onLoadMore: () => void
  onClearFilters: () => void
}

const EVENT_GROUP_LABELS: Record<EventGroupKey, string> = {
  today: '今天',
  yesterday: '昨天',
  this_week: '本周',
  earlier: '更早',
}

const EVENT_GROUP_ORDER: EventGroupKey[] = ['today', 'yesterday', 'this_week', 'earlier']

const OBJECT_TYPE_LABELS: Record<string, string> = {
  node: '节点',
  target: '目标',
}

function startOfDay(date: Date): Date {
  const next = new Date(date)
  next.setHours(0, 0, 0, 0)
  return next
}

function bucketKey(eventDate: Date, now: Date): EventGroupKey {
  const startToday = startOfDay(now)
  const startYesterday = new Date(startToday.getTime() - 24 * 60 * 60 * 1000)
  const day = startToday.getDay()
  const offsetToMonday = day === 0 ? 6 : day - 1
  const startWeek = new Date(startToday.getTime() - offsetToMonday * 24 * 60 * 60 * 1000)

  if (eventDate >= startToday) return 'today'
  if (eventDate >= startYesterday) return 'yesterday'
  if (eventDate >= startWeek) return 'this_week'
  return 'earlier'
}

function groupEventsByTime(
  events: StateChangeEventRecord[],
): Array<{ key: EventGroupKey; events: StateChangeEventRecord[] }> {
  const buckets: Record<EventGroupKey, StateChangeEventRecord[]> = {
    today: [],
    yesterday: [],
    this_week: [],
    earlier: [],
  }
  const now = new Date()
  for (const event of events) {
    const eventDate = new Date(event.created_at)
    if (Number.isNaN(eventDate.getTime())) {
      buckets.earlier.push(event)
      continue
    }
    buckets[bucketKey(eventDate, now)].push(event)
  }
  return EVENT_GROUP_ORDER.filter((key) => buckets[key].length > 0).map((key) => ({
    key,
    events: buckets[key],
  }))
}

function eventIcon(evt: StateChangeEventRecord): { cls: string; char: string } {
  if (evt.event_type === 'incident_recovered') return { cls: 'event-icon ei-ok', char: '✓' }
  if (evt.event_type === 'incident_escalated') return { cls: 'event-icon ei-err', char: '!' }
  return { cls: 'event-icon ei-warn', char: '!' }
}

function formatEventTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const now = new Date()
  const startToday = startOfDay(now)
  if (d >= startToday) {
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`
  }
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${min}`
}

function eventTypeLabel(value: StateChangeEventRecord['event_type']): string {
  return STATE_CHANGE_EVENT_TYPE_LABELS[value] ?? value
}

export function EventsStreamSection({
  events,
  exhausted,
  loadingMore,
  hasActiveFilters,
  onLoadMore,
  onClearFilters,
}: EventsStreamSectionProps) {
  const groupedEvents = groupEventsByTime(events)

  if (events.length === 0) {
    return (
      <div className="card" style={{ padding: '24px', textAlign: 'center' }}>
        <p style={{ fontSize: '13px', color: 'var(--t3)' }}>
          {hasActiveFilters ? '当前筛选没有匹配的事件' : '最近没有状态变更事件'}
        </p>
        {hasActiveFilters && (
          <button
            type="button"
            className="btn sm secondary"
            style={{ marginTop: '12px' }}
            onClick={onClearFilters}
          >
            重置筛选
          </button>
        )}
      </div>
    )
  }

  return (
    <>
      {groupedEvents.map((group, gi) => (
        <div key={group.key} className={`card animate-in d${gi + 1}`} style={{ marginBottom: '16px' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
            <h3 style={{ fontSize: '14px', fontWeight: 600, color: 'var(--t1)' }}>
              {EVENT_GROUP_LABELS[group.key]} ({group.events.length} 条)
            </h3>
          </div>
          {group.events.map((evt) => {
            const icon = eventIcon(evt)
            return (
              <div
                className="event-row"
                key={evt.event_id ?? `${evt.created_at}-${evt.incident_id}-${evt.event_type}`}
              >
                <span className="event-time">{formatEventTime(evt.created_at)}</span>
                <span className={icon.cls}>{icon.char}</span>
                <div className="event-body">
                  <div className="event-title">{eventTypeLabel(evt.event_type)}</div>
                  <div className="event-detail">
                    {evt.incident_class ? `${evt.incident_class} · ` : ''}
                    {evt.summary || '暂无摘要'}
                  </div>
                </div>
                <span className="event-target">
                  {OBJECT_TYPE_LABELS[evt.object_type] ?? evt.object_type} · {evt.object_id}
                </span>
              </div>
            )
          })}
        </div>
      ))}
      <button
        type="button"
        className="btn md secondary"
        style={{ width: '100%', justifyContent: 'center' }}
        onClick={onLoadMore}
        disabled={exhausted || loadingMore}
      >
        {loadingMore ? '正在加载…' : exhausted ? '无更多事件' : '加载更多'}
      </button>
    </>
  )
}
