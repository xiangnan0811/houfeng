import { Button, MonoDigits } from '../../components/atoms'
import { DetailSection } from '../../components/DetailSection'
import { EventList } from '../../components/EventList'
import type { StateChangeEventRecord } from '../../lib/types'

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

function startOfDay(date: Date): Date {
  const next = new Date(date)
  next.setHours(0, 0, 0, 0)
  return next
}

function bucketKey(eventDate: Date, now: Date): EventGroupKey {
  const startToday = startOfDay(now)
  const startYesterday = new Date(startToday.getTime() - 24 * 60 * 60 * 1000)
  // ISO week start: Monday. Use locale-independent logic.
  const day = startToday.getDay() // 0 (Sun) .. 6 (Sat)
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

export function EventsStreamSection({
  events,
  exhausted,
  loadingMore,
  hasActiveFilters,
  onLoadMore,
  onClearFilters,
}: EventsStreamSectionProps) {
  const groupedEvents = groupEventsByTime(events)

  return (
    <DetailSection
      eyebrow="事件流"
      title="事件流"
      ribbon={hasActiveFilters ? 'notice' : 'accent-2'}
      aside={<span><MonoDigits>{events.length}</MonoDigits> 条当前事件</span>}
    >
      <p className="events-stream-context">
        {hasActiveFilters
          ? '当前事件流只展示 URL 固定的筛选结果；加载更早事件会沿用同一组条件扩大数量上限。'
          : '默认事件流未限定时间范围，按最近事件数量截取；需要精确窗口可使用高级筛选。'}
      </p>
      {events.length === 0 ? (
        <EventList
          events={events}
          emptyTitle={hasActiveFilters ? '没有匹配的事件' : '最近没有状态变更事件'}
          emptyDescription={
            hasActiveFilters
              ? '当前 URL 筛选没有返回事件。重置筛选后再继续核对诊断时间线。'
              : '系统暂时没有新的状态变更事件。'
          }
          emptyAction={
            hasActiveFilters ? (
              <Button variant="ghost" size="md" onClick={onClearFilters}>
                重置筛选
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="probe-list">
          {groupedEvents.map((group) => (
            <div key={group.key} className="event-group">
              <header className="section-heading">
                <h3 className="section-heading__title">{EVENT_GROUP_LABELS[group.key]}</h3>
                <span className="section-heading__eyebrow">
                  <MonoDigits>{group.events.length}</MonoDigits>
                </span>
              </header>
              <EventList events={group.events} />
            </div>
          ))}
        </div>
      )}
      <div>
        <Button
          variant="secondary"
          size="sm"
          onClick={onLoadMore}
          disabled={exhausted || loadingMore}
        >
          {loadingMore ? '正在加载…' : exhausted ? '无更多事件' : '加载更早事件 ↓'}
        </Button>
      </div>
    </DetailSection>
  )
}
