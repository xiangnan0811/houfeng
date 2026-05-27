import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { FilterChip } from '../components/filters'
import { PageState } from '../components/PageState'
import { ApiError, listEvents } from '../lib/api'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type EventListFilter, type StateChangeEventType } from '../lib/types'
import { EventsFilterDrawer } from './events/EventsFilterDrawer'
import { EventsStreamSection } from './events/EventsStreamSection'
import {
  ALLOWED_EVENT_TYPES,
  ALLOWED_LIMITS,
  ALLOWED_TIME_RANGES,
  DEFAULT_FILTERS,
  DEFAULT_LIMIT,
  TIME_RANGE_DURATIONS_MS,
  TIME_RANGE_LABELS,
} from './events/eventsPageConstants'
import type { EventsPageState, FilterState, TimeRange } from './events/types'

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

function objectTypeLabel(value: string): string {
  if (value === 'node') return '节点'
  if (value === 'target') return '目标'
  return value
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
  const [limitState, setLimitState] = useState(() => ({
    filterKey: appliedFilterKey,
    effectiveLimit: filterLimit(appliedFilters),
  }))
  const effectiveLimit =
    limitState.filterKey === appliedFilterKey
      ? limitState.effectiveLimit
      : filterLimit(appliedFilters)
  const [state, setState] = useState<EventsPageState>({
    loading: true,
    error: null,
    events: [],
    exhausted: false,
  })
  const [loadingMore, setLoadingMore] = useState(false)
  const [filtersDrawerOpen, setFiltersDrawerOpen] = useState(false)

  const activeFilters = hasActiveFilters(appliedFilters)

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

  if (state.loading) {
    return <PageState kind="loading" title="正在加载事件…" />
  }

  if (state.error) {
    return (
      <PageState
        kind="error"
        eyebrow="事件"
        title="事件不可用"
        description={state.error}
        technicalSummary={state.error}
      />
    )
  }

  return (
    <div className="animate-in">
      <div className="page-header">
        <div>
          <h1 className="page-title">事件流</h1>
          <p className="page-sub">状态变更事件时间线</p>
        </div>
        <div className="header-actions">
          <button type="button" className="btn sm secondary" onClick={openFiltersDrawer}>
            筛选面板
          </button>
        </div>
      </div>

      {activeFilters && (
        <div className="filter-bar animate-in d1">
          {appliedFilters.time_range !== 'custom' && (
            <FilterChip
              label={`时间: ${TIME_RANGE_LABELS[appliedFilters.time_range]}`}
              onRemove={() => removeAppliedFilter('time_range')}
            />
          )}
          {appliedFilters.object_type && (
            <FilterChip
              label={`对象: ${objectTypeLabel(appliedFilters.object_type)}`}
              onRemove={() => removeAppliedFilter('object_type')}
            />
          )}
          {appliedFilters.severity && (
            <FilterChip
              label={`严重度: ${appliedFilters.severity}`}
              onRemove={() => removeAppliedFilter('severity')}
            />
          )}
          {appliedFilters.event_type && (
            <FilterChip
              label={`类型: ${STATE_CHANGE_EVENT_TYPE_LABELS[appliedFilters.event_type]}`}
              onRemove={() => removeAppliedFilter('event_type')}
            />
          )}
          {appliedFilters.limit !== String(DEFAULT_LIMIT) && (
            <FilterChip
              label={`数量: ${appliedFilters.limit}`}
              onRemove={() => removeAppliedFilter('limit')}
            />
          )}
          {appliedFilters.label && (
            <FilterChip
              label={`标签: ${appliedFilters.label}`}
              onRemove={() => removeAppliedFilter('label')}
            />
          )}
          {appliedFilters.notification_only && (
            <FilterChip label="仅通知事件" onRemove={() => removeAppliedFilter('notification_only')} />
          )}
          {appliedFilters.recovery_only && (
            <FilterChip label="仅恢复事件" onRemove={() => removeAppliedFilter('recovery_only')} />
          )}
          {appliedFilters.maintenance_only && (
            <FilterChip label="仅维护事件" onRemove={() => removeAppliedFilter('maintenance_only')} />
          )}
          {appliedFilters.include_backfilled && (
            <FilterChip label="含补传事件" onRemove={() => removeAppliedFilter('include_backfilled')} />
          )}
          <button type="button" className="filter-clear" onClick={() => commitFilters(DEFAULT_FILTERS)}>
            清除全部
          </button>
        </div>
      )}

      <EventsFilterDrawer
        open={filtersDrawerOpen}
        onClose={closeFiltersDrawer}
        filters={filters}
        onApply={applyDraftFilters}
        onReset={resetFilters}
        onTimeRangeChange={updateDraftTimeRange}
        onFilterChange={updateDraftFilter}
      />

      <div className="animate-in d2">
        <EventsStreamSection
          events={state.events}
          exhausted={state.exhausted}
          loadingMore={loadingMore}
          hasActiveFilters={activeFilters}
          onLoadMore={handleLoadMore}
          onClearFilters={() => commitFilters(DEFAULT_FILTERS)}
        />
      </div>
    </div>
  )
}
