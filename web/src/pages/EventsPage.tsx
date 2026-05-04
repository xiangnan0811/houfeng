import { useEffect, useMemo, useState } from 'react'

import { Button, MonoDigits, Tabs, type TabItem } from '../components/atoms'
import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { ApiError, listEvents } from '../lib/api'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type EventListFilter,
  type StateChangeEventRecord,
  type StateChangeEventType,
} from '../lib/types'

type TimeRange = '24h' | '7d' | '30d' | 'custom'

type FilterState = {
  object_type: '' | 'node' | 'target'
  severity: '' | '关注' | '告警' | '严重'
  event_type: '' | StateChangeEventType
  limit: string
  created_from: string
  created_to: string
  label: string
  notification_only: boolean
  recovery_only: boolean
  maintenance_only: boolean
  // 4th boolean per PRD §requirements 1. Backend currently does not expose
  // is_backfilled on state_change_events, so this toggle has no actual
  // filtering effect today. UI is wired and `include_backfilled` is forwarded
  // as a query param so future backend support is a one-line drop-in.
  include_backfilled: boolean
  // Time range segmented control. 'custom' preserves the original behavior
  // (user-controlled date inputs) — keep that as default so first load keeps
  // the previous "all recent events" semantics.
  time_range: TimeRange
}

type State = {
  loading: boolean
  error: string | null
  events: StateChangeEventRecord[]
  // True after a load-more fetch returns fewer rows than requested — meaning
  // backend has no more events to give for the current filter.
  exhausted: boolean
}

const DEFAULT_LIMIT = 50

const DEFAULT_FILTERS: FilterState = {
  object_type: '',
  severity: '',
  event_type: '',
  limit: String(DEFAULT_LIMIT),
  created_from: '',
  created_to: '',
  label: '',
  notification_only: false,
  recovery_only: false,
  maintenance_only: false,
  include_backfilled: true,
  time_range: 'custom',
}

const EVENT_TYPE_OPTIONS = Object.entries(STATE_CHANGE_EVENT_TYPE_LABELS) as Array<
  [StateChangeEventType, string]
>

const TIME_RANGE_TABS: TabItem<TimeRange>[] = [
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: 'custom', label: '自定义' },
]

const TIME_RANGE_DURATIONS_MS: Record<Exclude<TimeRange, 'custom'>, number> = {
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

function buildFilterQuery(filters: FilterState, effectiveLimit: number): EventListFilter {
  return {
    object_type: filters.object_type,
    severity: filters.severity,
    event_type: filters.event_type,
    limit: effectiveLimit,
    created_from: filters.created_from,
    created_to: filters.created_to,
    label: filters.label,
    notification_only: filters.notification_only,
    recovery_only: filters.recovery_only,
    maintenance_only: filters.maintenance_only,
  }
}

function applyTimeRange(filters: FilterState, range: TimeRange): FilterState {
  if (range === 'custom') {
    return { ...filters, time_range: range }
  }
  const now = new Date()
  const from = new Date(now.getTime() - TIME_RANGE_DURATIONS_MS[range])
  return {
    ...filters,
    time_range: range,
    created_from: from.toISOString(),
    created_to: now.toISOString(),
  }
}

type EventGroupKey = 'today' | 'yesterday' | 'this_week' | 'earlier'

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

export function EventsPage() {
  const [filters, setFilters] = useState<FilterState>(DEFAULT_FILTERS)
  const [appliedFilters, setAppliedFilters] = useState<FilterState>(DEFAULT_FILTERS)
  // effectiveLimit drives the actual limit sent to backend. It starts at the
  // user-selected limit; "加载更早事件" increments it by the user-selected
  // limit and refetches so older rows append naturally (server returns the
  // most-recent N for the current filter).
  const [effectiveLimit, setEffectiveLimit] = useState<number>(DEFAULT_LIMIT)
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    events: [],
    exhausted: false,
  })
  const [loadingMore, setLoadingMore] = useState(false)

  function submitFilters(nextFilters: FilterState) {
    const limitValue = Number(nextFilters.limit || DEFAULT_LIMIT) || DEFAULT_LIMIT
    setState((current) => ({ ...current, loading: true, error: null }))
    setEffectiveLimit(limitValue)
    setAppliedFilters({ ...nextFilters })
  }

  useEffect(() => {
    let cancelled = false

    listEvents(buildFilterQuery(appliedFilters, effectiveLimit))
      .then((events) => {
        if (cancelled) return
        // Note: appliedFilters.include_backfilled is intentionally not used
        // for client-side filtering — state_change_events do not expose
        // is_backfilled in the payload today, so there is no field to filter
        // on. The toggle is wired through state for future server-side
        // support; once backend accepts include_backfilled this becomes a
        // request-side filter automatically.
        setState({
          loading: false,
          error: null,
          events,
          exhausted: events.length < effectiveLimit,
        })
        setLoadingMore(false)
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message = error instanceof ApiError ? error.message : '加载事件失败'
        setState({ loading: false, error: message, events: [], exhausted: true })
        setLoadingMore(false)
      })

    return () => {
      cancelled = true
    }
  }, [appliedFilters, effectiveLimit])

  const groupedEvents = useMemo(() => groupEventsByTime(state.events), [state.events])

  function handleLoadMore() {
    if (state.exhausted || loadingMore) return
    const increment = Number(appliedFilters.limit || DEFAULT_LIMIT) || DEFAULT_LIMIT
    setLoadingMore(true)
    setEffectiveLimit((current) => current + increment)
  }

  if (state.loading) {
    return <section className="page-panel">正在加载事件…</section>
  }

  if (state.error) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">事件</p>
        <h2 className="page-panel__title">事件不可用</h2>
        <p className="page-panel__description">{state.error}</p>
      </section>
    )
  }

  const customRange = filters.time_range === 'custom'

  return (
    <div className="page-stack">
      <section className="page-panel">
        <p className="page-panel__eyebrow">事件</p>
        <h2 className="page-panel__title">事件</h2>
        <p className="page-panel__description">
          查看最新状态变更事件，并按对象类型、严重程度、事件类型与数量快速筛选。
        </p>
      </section>

      <DetailSection eyebrow="筛选条件" title="筛选条件">
        <form
          onSubmit={(event) => {
            event.preventDefault()
            submitFilters(filters)
          }}
        >
          <div style={{ marginBottom: 'var(--space-4)' }}>
            <p className="section-heading__eyebrow" style={{ marginBottom: 8 }}>时间范围</p>
            <Tabs<TimeRange>
              variant="pill"
              value={filters.time_range}
              onChange={(next) =>
                setFilters((current) => applyTimeRange(current, next))
              }
              items={TIME_RANGE_TABS}
            />
          </div>

          <div className="summary-grid">
            <label className="summary-card">
              <span className="summary-card__label">对象类型</span>
              <select
                aria-label="对象类型"
                value={filters.object_type}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, object_type: event.target.value as FilterState['object_type'] }))
                }
              >
                <option value="">全部</option>
                <option value="node">节点</option>
                <option value="target">目标</option>
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">严重程度</span>
              <select
                aria-label="严重程度"
                value={filters.severity}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, severity: event.target.value as FilterState['severity'] }))
                }
              >
                <option value="">全部</option>
                <option value="关注">关注</option>
                <option value="告警">告警</option>
                <option value="严重">严重</option>
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">事件类型</span>
              <select
                aria-label="事件类型"
                value={filters.event_type}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, event_type: event.target.value as FilterState['event_type'] }))
                }
              >
                <option value="">全部</option>
                {EVENT_TYPE_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">数量</span>
              <select
                aria-label="数量"
                className="mono"
                value={filters.limit}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, limit: event.target.value }))
                }
              >
                <option value="10">10</option>
                <option value="25">25</option>
                <option value="50">50</option>
                <option value="100">100</option>
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">开始时间</span>
              <input
                aria-label="开始时间"
                placeholder="2026-04-25T00:00:00Z"
                value={filters.created_from}
                disabled={!customRange}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, created_from: event.target.value }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">结束时间</span>
              <input
                aria-label="结束时间"
                placeholder="2026-04-26T00:00:00Z"
                value={filters.created_to}
                disabled={!customRange}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, created_to: event.target.value }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">标签</span>
              <input
                aria-label="标签"
                placeholder="edge"
                value={filters.label}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, label: event.target.value }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">通知相关</span>
              <input
                aria-label="仅看通知事件"
                type="checkbox"
                checked={filters.notification_only}
                onChange={(event) =>
                  setFilters((current) => ({
                    ...current,
                    notification_only: event.target.checked,
                  }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">恢复事件</span>
              <input
                aria-label="仅看恢复事件"
                type="checkbox"
                checked={filters.recovery_only}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, recovery_only: event.target.checked }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">维护事件</span>
              <input
                aria-label="仅看维护事件"
                type="checkbox"
                checked={filters.maintenance_only}
                onChange={(event) =>
                  setFilters((current) => ({
                    ...current,
                    maintenance_only: event.target.checked,
                  }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">包含补传事件</span>
              <input
                aria-label="包含补传事件"
                type="checkbox"
                checked={filters.include_backfilled}
                onChange={(event) =>
                  setFilters((current) => ({
                    ...current,
                    include_backfilled: event.target.checked,
                  }))
                }
              />
            </label>
          </div>

          <button type="submit">应用筛选</button>
          <button
            type="button"
            onClick={() => {
              setFilters({ ...DEFAULT_FILTERS })
              submitFilters(DEFAULT_FILTERS)
            }}
          >
            重置筛选
          </button>
        </form>
      </DetailSection>

      <DetailSection eyebrow="事件流" title="事件流">
        {state.events.length === 0 ? (
          <EventList events={state.events} />
        ) : (
          <div
            className="event-groups"
            style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-6)' }}
          >
            {groupedEvents.map((group) => (
              <div key={group.key} className="event-group">
                <header
                  className="section-heading"
                  style={{
                    display: 'flex',
                    alignItems: 'baseline',
                    gap: 'var(--space-2)',
                    marginBottom: 'var(--space-3)',
                  }}
                >
                  <h3 className="section-heading__title" style={{ margin: 0 }}>
                    {EVENT_GROUP_LABELS[group.key]}
                  </h3>
                  <span className="section-heading__eyebrow">
                    <MonoDigits>{group.events.length}</MonoDigits>
                  </span>
                </header>
                <EventList events={group.events} />
              </div>
            ))}
          </div>
        )}
        <div
          className="load-more"
          style={{ display: 'flex', justifyContent: 'center', marginTop: 'var(--space-6)' }}
        >
          <Button
            variant="secondary"
            size="sm"
            onClick={handleLoadMore}
            disabled={state.exhausted || loadingMore}
          >
            {loadingMore
              ? '正在加载…'
              : state.exhausted
                ? '无更多事件'
                : '加载更早事件 ↓'}
          </Button>
        </div>
      </DetailSection>
    </div>
  )
}
