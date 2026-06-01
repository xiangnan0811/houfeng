import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { Sparkline } from '../components/atoms'
import { PageState } from '../components/PageState'
import {
  ApiError,
  getDashboard,
  listEvents,
  listMonitoringInstances,
  listTargets,
} from '../lib/api'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type DashboardOverview,
  type EventListFilter,
  type StateChangeEventType,
} from '../lib/types'
import { EventsFilterDrawer } from './events/EventsFilterDrawer'
import { EventsFilterPanel } from './events/EventsFilterPanel'
import { EventsStreamSection } from './events/EventsStreamSection'
import {
  ALLOWED_EVENT_TYPES,
  ALLOWED_TIME_RANGES,
  DEFAULT_FILTERS,
  DEFAULT_LIMIT,
  PAGE_SIZE,
  TIME_RANGE_DURATIONS_MS,
} from './events/eventsPageConstants'
import type { EventsPageState, FilterState, TimeRange } from './events/types'

function isObjectType(value: string | null): value is FilterState['object_type'] {
  return value === 'monitoring_instance' || value === 'target'
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
  if (/[zZ]$|[+-]\d{2}:\d{2}$/.test(trimmed)) return trimmed
  return new Date(trimmed).toISOString()
}

function parseEventSearchParams(searchParams: URLSearchParams): FilterState {
  const rawTimeRange = searchParams.get('time_range')
  const timeRange = isTimeRange(rawTimeRange) ? rawTimeRange : DEFAULT_FILTERS.time_range
  const customRange = timeRange === 'custom'
  const rawObjectType = searchParams.get('object_type')
  const rawSeverity = searchParams.get('severity')
  const rawEventType = searchParams.get('event_type')

  return {
    object_type: isObjectType(rawObjectType) ? rawObjectType : DEFAULT_FILTERS.object_type,
    severity: isSeverity(rawSeverity) ? rawSeverity : DEFAULT_FILTERS.severity,
    event_type: isEventType(rawEventType) ? rawEventType : DEFAULT_FILTERS.event_type,
    limit: String(DEFAULT_LIMIT),
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
    incident_class: (searchParams.get('incident_class') ?? '').trim(),
    keyword: (searchParams.get('keyword') ?? '').trim(),
  }
}

function normalizeFilters(filters: FilterState): FilterState {
  const timeRange = ALLOWED_TIME_RANGES.has(filters.time_range)
    ? filters.time_range
    : DEFAULT_FILTERS.time_range
  const customRange = timeRange === 'custom'

  return {
    object_type: isObjectType(filters.object_type) ? filters.object_type : '',
    severity: isSeverity(filters.severity) ? filters.severity : '',
    event_type: isEventType(filters.event_type) ? filters.event_type : '',
    limit: String(DEFAULT_LIMIT),
    created_from: customRange ? normalizeDateInput(filters.created_from) : '',
    created_to: customRange ? normalizeDateInput(filters.created_to) : '',
    label: filters.label.trim(),
    notification_only: filters.notification_only,
    recovery_only: filters.recovery_only,
    maintenance_only: filters.maintenance_only,
    include_backfilled: filters.include_backfilled,
    time_range: timeRange,
    incident_class: filters.incident_class.trim(),
    keyword: filters.keyword.trim(),
  }
}

function searchParamsFromFilters(filters: FilterState): URLSearchParams {
  const normalized = normalizeFilters(filters)
  const next = new URLSearchParams()
  if (normalized.object_type) next.set('object_type', normalized.object_type)
  if (normalized.severity) next.set('severity', normalized.severity)
  if (normalized.event_type) next.set('event_type', normalized.event_type)
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
  if (normalized.incident_class) next.set('incident_class', normalized.incident_class)
  if (normalized.keyword) next.set('keyword', normalized.keyword)
  return next
}

function filterKey(filters: FilterState): string {
  return searchParamsFromFilters(filters).toString()
}

function hasActiveFilters(filters: FilterState): boolean {
  const normalized = normalizeFilters(filters)
  return (
    normalized.object_type !== '' ||
    normalized.severity !== '' ||
    normalized.event_type !== '' ||
    normalized.created_from !== '' ||
    normalized.created_to !== '' ||
    normalized.label !== '' ||
    normalized.notification_only ||
    normalized.recovery_only ||
    normalized.maintenance_only ||
    normalized.include_backfilled ||
    normalized.time_range !== 'custom' ||
    normalized.incident_class !== '' ||
    normalized.keyword !== ''
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

function applyLocalFilters(
  events: import('../lib/types').StateChangeEventRecord[],
  incidentClass: string,
  keyword: string,
): import('../lib/types').StateChangeEventRecord[] {
  let result = events
  if (incidentClass) {
    result = result.filter((e) => e.incident_class === incidentClass)
  }
  if (keyword) {
    const kw = keyword.toLowerCase()
    result = result.filter(
      (e) =>
        e.summary.toLowerCase().includes(kw) ||
        e.incident_class.toLowerCase().includes(kw),
    )
  }
  return result
}

function exportCsv(
  events: import('../lib/types').StateChangeEventRecord[],
  nameMap: Map<string, string>,
) {
  const header = '时间,严重度,事件类型,异常类别,摘要,对象类型,对象名称'
  const rows = events.map((e) => {
    const name = nameMap.get(e.object_id) || e.object_id
    const cols = [
      e.created_at,
      e.severity,
      STATE_CHANGE_EVENT_TYPE_LABELS[e.event_type] ?? e.event_type,
      e.incident_class,
      `"${(e.summary || '').replace(/"/g, '""')}"`,
      e.object_type,
      name,
    ]
    return cols.join(',')
  })
  const csv = [header, ...rows].join('\n')
  const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  const date = new Date().toISOString().slice(0, 10)
  a.href = url
  a.download = `events-export-${date}.csv`
  a.click()
  URL.revokeObjectURL(url)
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
  const [effectiveLimit, setEffectiveLimit] = useState(DEFAULT_LIMIT)
  const [state, setState] = useState<EventsPageState>({
    loading: true,
    error: null,
    events: [],
    exhausted: false,
  })
  const [loadingMore, setLoadingMore] = useState(false)
  const [filtersDrawerOpen, setFiltersDrawerOpen] = useState(false)
  const [page, setPage] = useState(() => {
    const p = Number(searchParams.get('page'))
    return p > 0 ? p : 1
  })
  const [nameMap, setNameMap] = useState<Map<string, string>>(new Map())
  const [dashboard, setDashboard] = useState<DashboardOverview | null>(null)

  const activeFilters = hasActiveFilters(appliedFilters)

  const filteredEvents = useMemo(
    () => applyLocalFilters(state.events, appliedFilters.incident_class, appliedFilters.keyword),
    [state.events, appliedFilters.incident_class, appliedFilters.keyword],
  )

  const totalPages = Math.max(1, Math.ceil(filteredEvents.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)

  // Load name map and dashboard on mount
  useEffect(() => {
    Promise.all([listMonitoringInstances(), listTargets()]).then(([monitoring, targets]) => {
      const map = new Map<string, string>()
      for (const n of monitoring) map.set(n.monitoring_instance_id, n.display_name)
      for (const t of targets) map.set(t.target_id, t.name)
      setNameMap(map)
    }).catch(() => {})

    getDashboard().then(setDashboard).catch(() => {})
  }, [])

  // Sync URL params
  useEffect(() => {
    const canonicalParams = searchParamsFromFilters(appliedFilters)
    if (currentPage > 1) canonicalParams.set('page', String(currentPage))
    if (searchParams.toString() !== canonicalParams.toString()) {
      setSearchParams(canonicalParams, { replace: true })
    }
  }, [appliedFilterKey, appliedFilters, currentPage, searchParams, setSearchParams])

  function commitFilters(nextFilters: FilterState) {
    const normalized = normalizeFilters(nextFilters)
    const nextKey = filterKey(normalized)
    const nextParams = searchParamsFromFilters(normalized)
    if (nextKey !== appliedFilterKey) {
      setState((current) => ({ ...current, loading: true, error: null }))
      setEffectiveLimit(DEFAULT_LIMIT)
    }
    setDraftState({ filterKey: nextKey, filters: normalized })
    setLoadingMore(false)
    setPage(1)
    if (searchParams.toString() !== nextParams.toString()) {
      setSearchParams(nextParams, { replace: true })
    }
  }

  // Fetch events when API filters change
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
    return () => { cancelled = true }
  }, [appliedFilterKey, appliedFilters, effectiveLimit])

  function handleLoadMore() {
    if (state.exhausted || loadingMore) return
    setLoadingMore(true)
    setEffectiveLimit((prev) => prev + DEFAULT_LIMIT)
  }

  const handlePageChange = useCallback((p: number) => {
    setPage(p)
    const params = searchParamsFromFilters(appliedFilters)
    if (p > 1) params.set('page', String(p))
    setSearchParams(params, { replace: true })
  }, [appliedFilters, setSearchParams])

  function commitInlineFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    commitFilters({ ...appliedFilters, [key]: value })
  }

  function commitInlineTimeRange(range: TimeRange) {
    commitFilters(applyTimeRange(appliedFilters, range))
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
          <button
            type="button"
            className="btn sm secondary"
            onClick={() => exportCsv(filteredEvents, nameMap)}
            disabled={filteredEvents.length === 0}
          >
            导出 CSV
          </button>
          <button type="button" className="btn sm secondary" onClick={openFiltersDrawer}>
            高级筛选
          </button>
        </div>
      </div>

      {dashboard && (
        <div className="hero-stats animate-in d1">
          <div className="hero-stat">
            <span className="hs-label">新增异常 (24h)</span>
            <span className="hs-value">{dashboard.recent_new_incident_count}</span>
            {dashboard.new_incident_trend_24h && (
              <Sparkline values={dashboard.new_incident_trend_24h} tone="alert" />
            )}
          </div>
          <div className="hero-stat">
            <span className="hs-label">已恢复 (24h)</span>
            <span className="hs-value">{dashboard.recent_recovery_count}</span>
            {dashboard.recovery_trend_24h && (
              <Sparkline values={dashboard.recovery_trend_24h} tone="normal" />
            )}
          </div>
        </div>
      )}

      <div className="animate-in d1">
        <EventsFilterPanel
          filters={appliedFilters}
          onFilterChange={commitInlineFilter}
          onTimeRangeChange={commitInlineTimeRange}
        />
      </div>

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
          events={filteredEvents}
          exhausted={state.exhausted}
          loadingMore={loadingMore}
          hasActiveFilters={activeFilters}
          page={currentPage}
          nameMap={nameMap}
          onPageChange={handlePageChange}
          onLoadMore={handleLoadMore}
          onClearFilters={() => commitFilters(DEFAULT_FILTERS)}
        />
      </div>
    </div>
  )
}
