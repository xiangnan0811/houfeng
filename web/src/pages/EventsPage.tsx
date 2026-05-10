import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { Button, Drawer, MonoDigits, Tabs, type TabItem } from '../components/atoms'
import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import {
  FilterBar,
  FilterChip,
  FilterSelect,
  FilterToggle,
  type FilterSelectOption,
} from '../components/filters'
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
const LIMIT_OPTIONS = ['10', '25', '50', '100'] as const
const OBJECT_TYPE_OPTIONS: FilterSelectOption[] = [
  { value: 'node', label: '节点' },
  { value: 'target', label: '目标' },
]
const SEVERITY_OPTIONS: FilterSelectOption[] = [
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
]
const LIMIT_SELECT_OPTIONS: FilterSelectOption[] = LIMIT_OPTIONS.map((value) => ({
  value,
  label: value,
}))

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
  include_backfilled: false,
  time_range: 'custom',
}

const EVENT_TYPE_OPTIONS = Object.entries(STATE_CHANGE_EVENT_TYPE_LABELS) as Array<
  [StateChangeEventType, string]
>
const EVENT_TYPE_SELECT_OPTIONS: FilterSelectOption[] = EVENT_TYPE_OPTIONS.map(
  ([value, label]) => ({ value, label }),
)

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

const TIME_RANGE_LABELS: Record<TimeRange, string> = {
  '24h': '近 24 小时',
  '7d': '近 7 天',
  '30d': '近 30 天',
  custom: '自定义',
}

const ALLOWED_EVENT_TYPES = new Set<StateChangeEventType>(
  EVENT_TYPE_OPTIONS.map(([value]) => value),
)
const ALLOWED_LIMITS = new Set<string>(LIMIT_OPTIONS)
const ALLOWED_TIME_RANGES = new Set<TimeRange>(['24h', '7d', '30d', 'custom'])

function isObjectType(value: string | null): value is 'node' | 'target' {
  return value === 'node' || value === 'target'
}

function isSeverity(value: string | null): value is FilterState['severity'] {
  return value === '关注' || value === '告警' || value === '严重'
}

function isEventType(value: string | null): value is StateChangeEventType {
  return value !== null && ALLOWED_EVENT_TYPES.has(value as StateChangeEventType)
}

function isTimeRange(value: string | null): value is TimeRange {
  return value !== null && ALLOWED_TIME_RANGES.has(value as TimeRange)
}

function isValidDateInput(value: string): boolean {
  return value.trim() !== '' && !Number.isNaN(Date.parse(value.trim()))
}

function normalizeDateInput(value: string): string {
  const trimmed = value.trim()
  return isValidDateInput(trimmed) ? trimmed : ''
}

function normalizeDateForApi(value: string): string {
  const trimmed = normalizeDateInput(value)
  if (!trimmed) return ''
  if (/[zZ]$|[+-]\d{2}:\d{2}$/.test(trimmed)) {
    return trimmed
  }
  return new Date(trimmed).toISOString()
}

function parseEventSearchParams(searchParams: URLSearchParams): FilterState {
  const rawTimeRange = searchParams.get('time_range')
  const timeRange = isTimeRange(rawTimeRange) ? rawTimeRange : DEFAULT_FILTERS.time_range
  const customRange = timeRange === 'custom'
  const rawObjectType = searchParams.get('object_type')
  const rawSeverity = searchParams.get('severity')
  const rawEventType = searchParams.get('event_type')
  const rawLimit = searchParams.get('limit')

  return {
    object_type: isObjectType(rawObjectType) ? rawObjectType : DEFAULT_FILTERS.object_type,
    severity: isSeverity(rawSeverity) ? rawSeverity : DEFAULT_FILTERS.severity,
    event_type: isEventType(rawEventType) ? rawEventType : DEFAULT_FILTERS.event_type,
    limit: ALLOWED_LIMITS.has(rawLimit ?? '') ? rawLimit ?? DEFAULT_FILTERS.limit : DEFAULT_FILTERS.limit,
    created_from: customRange
      ? normalizeDateInput(searchParams.get('created_from') ?? '')
      : DEFAULT_FILTERS.created_from,
    created_to: customRange
      ? normalizeDateInput(searchParams.get('created_to') ?? '')
      : DEFAULT_FILTERS.created_to,
    label: (searchParams.get('label') ?? '').trim(),
    notification_only: searchParams.get('notification_only') === '1',
    recovery_only: searchParams.get('recovery_only') === '1',
    maintenance_only: searchParams.get('maintenance_only') === '1',
    include_backfilled: searchParams.get('include_backfilled') === '1',
    time_range: timeRange,
  }
}

function normalizeFilters(filters: FilterState): FilterState {
  const timeRange = ALLOWED_TIME_RANGES.has(filters.time_range)
    ? filters.time_range
    : DEFAULT_FILTERS.time_range
  const customRange = timeRange === 'custom'
  const limit = ALLOWED_LIMITS.has(filters.limit) ? filters.limit : DEFAULT_FILTERS.limit

  return {
    object_type: isObjectType(filters.object_type) ? filters.object_type : '',
    severity: isSeverity(filters.severity) ? filters.severity : '',
    event_type: isEventType(filters.event_type) ? filters.event_type : '',
    limit,
    created_from: customRange ? normalizeDateInput(filters.created_from) : '',
    created_to: customRange ? normalizeDateInput(filters.created_to) : '',
    label: filters.label.trim(),
    notification_only: filters.notification_only,
    recovery_only: filters.recovery_only,
    maintenance_only: filters.maintenance_only,
    include_backfilled: filters.include_backfilled,
    time_range: timeRange,
  }
}

function searchParamsFromFilters(filters: FilterState): URLSearchParams {
  const normalized = normalizeFilters(filters)
  const next = new URLSearchParams()
  if (normalized.object_type) next.set('object_type', normalized.object_type)
  if (normalized.severity) next.set('severity', normalized.severity)
  if (normalized.event_type) next.set('event_type', normalized.event_type)
  if (normalized.limit !== String(DEFAULT_LIMIT)) next.set('limit', normalized.limit)
  if (normalized.time_range !== 'custom') {
    next.set('time_range', normalized.time_range)
  } else if (normalized.created_from || normalized.created_to) {
    next.set('time_range', 'custom')
    if (normalized.created_from) next.set('created_from', normalized.created_from)
    if (normalized.created_to) next.set('created_to', normalized.created_to)
  }
  if (normalized.label) next.set('label', normalized.label)
  if (normalized.notification_only) next.set('notification_only', '1')
  if (normalized.recovery_only) next.set('recovery_only', '1')
  if (normalized.maintenance_only) next.set('maintenance_only', '1')
  if (normalized.include_backfilled) next.set('include_backfilled', '1')
  return next
}

function filterKey(filters: FilterState): string {
  return searchParamsFromFilters(filters).toString()
}

function filterLimit(filters: FilterState): number {
  return Number(filters.limit || DEFAULT_LIMIT) || DEFAULT_LIMIT
}

function hasActiveFilters(filters: FilterState): boolean {
  const normalized = normalizeFilters(filters)
  return (
    normalized.object_type !== '' ||
    normalized.severity !== '' ||
    normalized.event_type !== '' ||
    normalized.limit !== String(DEFAULT_LIMIT) ||
    normalized.created_from !== '' ||
    normalized.created_to !== '' ||
    normalized.label !== '' ||
    normalized.notification_only ||
    normalized.recovery_only ||
    normalized.maintenance_only ||
    normalized.include_backfilled ||
    normalized.time_range !== 'custom'
  )
}

function buildFilterQuery(filters: FilterState, effectiveLimit: number): EventListFilter {
  const query: EventListFilter = {
    object_type: filters.object_type,
    severity: filters.severity,
    event_type: filters.event_type,
    limit: effectiveLimit,
    label: filters.label,
    notification_only: filters.notification_only,
    recovery_only: filters.recovery_only,
    maintenance_only: filters.maintenance_only,
    include_backfilled: filters.include_backfilled,
  }

  if (filters.time_range === 'custom') {
    query.created_from = normalizeDateForApi(filters.created_from)
    query.created_to = normalizeDateForApi(filters.created_to)
    return query
  }

  const now = new Date()
  const from = new Date(now.getTime() - TIME_RANGE_DURATIONS_MS[filters.time_range])
  query.created_from = from.toISOString()
  query.created_to = now.toISOString()
  return query
}

function applyTimeRange(filters: FilterState, range: TimeRange): FilterState {
  return {
    ...filters,
    time_range: range,
    created_from: range === 'custom' ? filters.created_from : '',
    created_to: range === 'custom' ? filters.created_to : '',
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
  const [searchParams, setSearchParams] = useSearchParams()
  const appliedFilterKey = useMemo(
    () => filterKey(parseEventSearchParams(searchParams)),
    [searchParams],
  )
  const appliedFilters = useMemo(
    () => normalizeFilters(parseEventSearchParams(new URLSearchParams(appliedFilterKey))),
    [appliedFilterKey],
  )
  const [draftState, setDraftState] = useState(() => ({
    filterKey: appliedFilterKey,
    filters: appliedFilters,
  }))
  const filters =
    draftState.filterKey === appliedFilterKey ? draftState.filters : appliedFilters
  // effectiveLimit drives the actual limit sent to backend. It starts at the
  // user-selected limit; "加载更早事件" increments it by the user-selected
  // limit and refetches so older rows append naturally (server returns the
  // most-recent N for the current filter).
  const [limitState, setLimitState] = useState(() => ({
    filterKey: appliedFilterKey,
    effectiveLimit: filterLimit(appliedFilters),
  }))
  const effectiveLimit =
    limitState.filterKey === appliedFilterKey
      ? limitState.effectiveLimit
      : filterLimit(appliedFilters)
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    events: [],
    exhausted: false,
  })
  const [loadingMore, setLoadingMore] = useState(false)
  const [filtersDrawerOpen, setFiltersDrawerOpen] = useState(false)

  useEffect(() => {
    const canonicalParams = searchParamsFromFilters(appliedFilters)
    if (searchParams.toString() !== canonicalParams.toString()) {
      setSearchParams(canonicalParams, { replace: true })
    }
  }, [appliedFilterKey, appliedFilters, searchParams, setSearchParams])

  function commitFilters(nextFilters: FilterState) {
    const normalized = normalizeFilters(nextFilters)
    const nextKey = filterKey(normalized)
    const nextLimit = filterLimit(normalized)
    const nextParams = searchParamsFromFilters(normalized)
    if (nextKey !== appliedFilterKey || nextLimit !== effectiveLimit) {
      setState((current) => ({ ...current, loading: true, error: null }))
    }
    setDraftState({ filterKey: nextKey, filters: normalized })
    setLoadingMore(false)
    setLimitState({ filterKey: nextKey, effectiveLimit: nextLimit })
    if (searchParams.toString() !== nextParams.toString()) {
      setSearchParams(nextParams, { replace: true })
    }
  }

  useEffect(() => {
    let cancelled = false

    listEvents(buildFilterQuery(appliedFilters, effectiveLimit))
      .then((events) => {
        if (cancelled) return
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
  }, [appliedFilters, appliedFilterKey, effectiveLimit])

  const groupedEvents = useMemo(() => groupEventsByTime(state.events), [state.events])
  const activeFilters = hasActiveFilters(appliedFilters)

  function handleLoadMore() {
    if (state.exhausted || loadingMore) return
    const increment = filterLimit(appliedFilters)
    setLoadingMore(true)
    setLimitState({
      filterKey: appliedFilterKey,
      effectiveLimit: effectiveLimit + increment,
    })
  }

  function updateDraftFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    setDraftState((current) => {
      const currentFilters =
        current.filterKey === appliedFilterKey ? current.filters : appliedFilters
      return {
        filterKey: appliedFilterKey,
        filters: { ...currentFilters, [key]: value },
      }
    })
  }

  function updateDraftTimeRange(next: TimeRange) {
    setDraftState((current) => {
      const currentFilters =
        current.filterKey === appliedFilterKey ? current.filters : appliedFilters
      return {
        filterKey: appliedFilterKey,
        filters: applyTimeRange(currentFilters, next),
      }
    })
  }

  function openFiltersDrawer() {
    setDraftState({ filterKey: appliedFilterKey, filters: appliedFilters })
    setFiltersDrawerOpen(true)
  }

  function closeFiltersDrawer() {
    setDraftState({ filterKey: appliedFilterKey, filters: appliedFilters })
    setFiltersDrawerOpen(false)
  }

  function applyDraftFilters() {
    commitFilters(filters)
    setFiltersDrawerOpen(false)
  }

  function resetFilters() {
    commitFilters(DEFAULT_FILTERS)
    setFiltersDrawerOpen(false)
  }

  function removeAppliedFilter(key: keyof FilterState) {
    commitFilters({
      ...appliedFilters,
      [key]: DEFAULT_FILTERS[key],
    })
  }

  const objectTypeLabel =
    appliedFilters.object_type === 'node'
      ? '节点'
      : appliedFilters.object_type === 'target'
        ? '目标'
        : ''

  const activeFilterChips = (
    <>
      {appliedFilters.object_type ? (
        <FilterChip
          label={`对象类型: ${objectTypeLabel}`}
          onRemove={() => removeAppliedFilter('object_type')}
        />
      ) : null}
      {appliedFilters.severity ? (
        <FilterChip
          label={`严重程度: ${appliedFilters.severity}`}
          onRemove={() => removeAppliedFilter('severity')}
        />
      ) : null}
      {appliedFilters.event_type ? (
        <FilterChip
          label={`事件类型: ${STATE_CHANGE_EVENT_TYPE_LABELS[appliedFilters.event_type]}`}
          onRemove={() => removeAppliedFilter('event_type')}
        />
      ) : null}
      {appliedFilters.limit !== String(DEFAULT_LIMIT) ? (
        <FilterChip
          label={`数量: ${appliedFilters.limit}`}
          onRemove={() => removeAppliedFilter('limit')}
        />
      ) : null}
      {appliedFilters.time_range !== 'custom' ? (
        <FilterChip
          label={`时间范围: ${TIME_RANGE_LABELS[appliedFilters.time_range]}`}
          onRemove={() => removeAppliedFilter('time_range')}
        />
      ) : null}
      {appliedFilters.created_from ? (
        <FilterChip
          label={`开始时间: ${appliedFilters.created_from}`}
          onRemove={() => removeAppliedFilter('created_from')}
        />
      ) : null}
      {appliedFilters.created_to ? (
        <FilterChip
          label={`结束时间: ${appliedFilters.created_to}`}
          onRemove={() => removeAppliedFilter('created_to')}
        />
      ) : null}
      {appliedFilters.label ? (
        <FilterChip
          label={`标签: ${appliedFilters.label}`}
          onRemove={() => removeAppliedFilter('label')}
        />
      ) : null}
      {appliedFilters.notification_only ? (
        <FilterChip
          label="仅看通知事件"
          onRemove={() => removeAppliedFilter('notification_only')}
        />
      ) : null}
      {appliedFilters.recovery_only ? (
        <FilterChip
          label="仅看恢复事件"
          onRemove={() => removeAppliedFilter('recovery_only')}
        />
      ) : null}
      {appliedFilters.maintenance_only ? (
        <FilterChip
          label="仅看维护事件"
          onRemove={() => removeAppliedFilter('maintenance_only')}
        />
      ) : null}
      {appliedFilters.include_backfilled ? (
        <FilterChip
          label="包含补传事件"
          onRemove={() => removeAppliedFilter('include_backfilled')}
        />
      ) : null}
    </>
  )

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
    <div className="page-stack events-page">
      <section className="page-panel">
        <p className="page-panel__eyebrow">事件</p>
        <h2 className="page-panel__title">事件</h2>
        <p className="page-panel__description">
          查看最新状态变更事件，并按对象类型、严重程度、事件类型与数量快速筛选。
        </p>
      </section>

      <DetailSection eyebrow="筛选条件" title="筛选条件">
        <FilterBar
          className="events-filter-overview"
          hasActiveFilters={activeFilters}
          onClearAll={() => commitFilters(DEFAULT_FILTERS)}
          activeChips={activeFilterChips}
        >
          <div className="events-filter-overview__status">
            <span className="events-filter-overview__label">当前筛选</span>
            <span className="events-filter-overview__value">
              {activeFilters ? '已应用筛选条件' : '默认事件流'}
            </span>
          </div>
          <Button variant="secondary" size="sm" onClick={openFiltersDrawer}>
            高级筛选
          </Button>
        </FilterBar>
      </DetailSection>

      <Drawer
        open={filtersDrawerOpen}
        onClose={closeFiltersDrawer}
        title="事件筛选"
        ariaLabel="事件高级筛选"
      >
        <form
          className="events-filter-drawer"
          onSubmit={(event) => {
            event.preventDefault()
            applyDraftFilters()
          }}
        >
          <div className="events-filter-drawer__group">
            <span className="events-filter-drawer__label">时间范围</span>
            <Tabs<TimeRange>
              variant="pill"
              value={filters.time_range}
              onChange={updateDraftTimeRange}
              items={TIME_RANGE_TABS}
            />
          </div>

          <div className="events-filter-drawer__grid">
            <FilterSelect
              label="对象类型"
              value={filters.object_type || null}
              options={OBJECT_TYPE_OPTIONS}
              onChange={(value) => updateDraftFilter('object_type', value === 'node' || value === 'target' ? value : '')}
            />
            <FilterSelect
              label="严重程度"
              value={filters.severity || null}
              options={SEVERITY_OPTIONS}
              onChange={(value) =>
                updateDraftFilter(
                  'severity',
                  value === '关注' || value === '告警' || value === '严重' ? value : '',
                )
              }
            />
            <FilterSelect
              label="事件类型"
              value={filters.event_type || null}
              options={EVENT_TYPE_SELECT_OPTIONS}
              onChange={(value) =>
                updateDraftFilter(
                  'event_type',
                  isEventType(value) ? value : '',
                )
              }
            />
            <FilterSelect
              label="数量"
              value={filters.limit}
              options={LIMIT_SELECT_OPTIONS}
              onChange={(value) =>
                updateDraftFilter(
                  'limit',
                  value !== null && ALLOWED_LIMITS.has(value) ? value : String(DEFAULT_LIMIT),
                )
              }
            />
            <FilterToggle
              label="仅看通知事件"
              checked={filters.notification_only}
              onChange={(checked) => updateDraftFilter('notification_only', checked)}
            />
            <FilterToggle
              label="仅看恢复事件"
              checked={filters.recovery_only}
              onChange={(checked) => updateDraftFilter('recovery_only', checked)}
            />
            <FilterToggle
              label="仅看维护事件"
              checked={filters.maintenance_only}
              onChange={(checked) => updateDraftFilter('maintenance_only', checked)}
            />
            <FilterToggle
              label="包含补传事件"
              checked={filters.include_backfilled}
              onChange={(checked) => updateDraftFilter('include_backfilled', checked)}
            />
          </div>

          <div className="events-filter-drawer__fields">
            <label className="events-filter-drawer__field">
              <span className="events-filter-drawer__label">开始时间</span>
              <input
                aria-label="开始时间"
                placeholder="2026-04-25T00:00:00Z"
                value={filters.created_from}
                disabled={!customRange}
                onChange={(event) =>
                  updateDraftFilter('created_from', event.target.value)
                }
              />
            </label>

            <label className="events-filter-drawer__field">
              <span className="events-filter-drawer__label">结束时间</span>
              <input
                aria-label="结束时间"
                placeholder="2026-04-26T00:00:00Z"
                value={filters.created_to}
                disabled={!customRange}
                onChange={(event) =>
                  updateDraftFilter('created_to', event.target.value)
                }
              />
            </label>

            <label className="events-filter-drawer__field">
              <span className="events-filter-drawer__label">标签</span>
              <input
                aria-label="标签"
                placeholder="edge"
                value={filters.label}
                onChange={(event) =>
                  updateDraftFilter('label', event.target.value)
                }
              />
            </label>

            <div className="events-filter-drawer__field">
              <span className="events-filter-drawer__label">包含补传事件</span>
              <span className="events-filter-drawer__value">
                {filters.include_backfilled ? '已包含' : '未包含'}
              </span>
              <span className="events-filter-drawer__hint">
                {filters.include_backfilled ? '补传相关事件会进入列表' : '默认隐藏补传相关事件'}
              </span>
            </div>
          </div>

          <div className="events-filter-drawer__actions">
            <Button type="submit" size="sm">
              应用筛选
            </Button>
            <Button type="button" variant="secondary" size="sm" onClick={resetFilters}>
              重置筛选
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={closeFiltersDrawer}>
              关闭
            </Button>
          </div>
        </form>
      </Drawer>

      <DetailSection eyebrow="事件流" title="事件流">
        {state.events.length === 0 ? (
          <EventList events={state.events} />
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
