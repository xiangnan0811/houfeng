import type {
  MonitoringInstanceFilterState,
  MonitoringInstanceQuickView,
  MonitoringInstanceSortKey,
  MonitoringInstanceSortState,
} from './types'

export const MONITORING_LIST_PATH = '/monitoring'

const MONITORING_LIST_HREF = /^\/monitoring(?:\?[^#]*)?$/

const QUICK_VIEWS: readonly MonitoringInstanceQuickView[] = [
  'all',
  'abnormal',
  'onboarding',
  'runtime-attention',
  'binding-conflict',
]

const SORT_KEYS: readonly MonitoringInstanceSortKey[] = [
  'identity',
  'issue',
  'location',
  'health',
  'heartbeat',
]

export const MONITORING_FILTER_QUERY_KEYS = [
  'group',
  'region',
  'city',
  'provider',
  'lifecycle',
  'run_status',
  'health',
  'labels',
] as const

const EMPTY_FILTERS: MonitoringInstanceFilterState = {
  group: null,
  region: null,
  city: null,
  provider: null,
  lifecycle: null,
  runStatus: null,
  health: null,
  labels: [],
}

export const MONITORING_SELECTED_IDS_QUERY_KEY = 'selected'

const MONITORING_INSTANCE_ID_PATTERN = /^mi_[A-Za-z0-9][A-Za-z0-9_-]*$/

export function parseMonitoringQuickView(searchParams: URLSearchParams): MonitoringInstanceQuickView {
  const view = searchParams.get('view')
  if (view && QUICK_VIEWS.includes(view as MonitoringInstanceQuickView)) {
    return view as MonitoringInstanceQuickView
  }
  if (searchParams.get('abnormal') === '1') return 'abnormal'
  if (searchParams.get('onboarding') === 'pending') return 'onboarding'
  return 'all'
}

export function parseMonitoringSearchQuery(searchParams: URLSearchParams): string {
  return searchParams.get('q') ?? ''
}

export function parseMonitoringSortState(searchParams: URLSearchParams): MonitoringInstanceSortState | null {
  const key = searchParams.get('sort')
  if (!key || !SORT_KEYS.includes(key as MonitoringInstanceSortKey)) return null
  const direction = searchParams.get('dir') === 'desc' ? 'desc' : 'asc'
  return { key: key as MonitoringInstanceSortKey, direction }
}

export function parseMonitoringFilters(searchParams: URLSearchParams): MonitoringInstanceFilterState {
  return {
    group: emptyToNull(searchParams.get('group')),
    region: emptyToNull(searchParams.get('region')),
    city: emptyToNull(searchParams.get('city')),
    provider: emptyToNull(searchParams.get('provider')),
    lifecycle: emptyToNull(searchParams.get('lifecycle')),
    runStatus: emptyToNull(searchParams.get('run_status')),
    health: emptyToNull(searchParams.get('health')),
    labels: parseMultiValue(searchParams.get('labels')),
  }
}

export function parseMultiValue(value: string | null): string[] {
  if (!value) return []
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}
export function parseMonitoringSelectedIds(searchParams: URLSearchParams): string[] {
  return normalizeMonitoringSelectedIds(
    searchParams.getAll(MONITORING_SELECTED_IDS_QUERY_KEY).flatMap((value) => parseMultiValue(value)),
  )
}

export function writeMonitoringSelectedIds(params: URLSearchParams, ids: readonly string[]) {
  params.delete(MONITORING_SELECTED_IDS_QUERY_KEY)
  for (const id of normalizeMonitoringSelectedIds(ids)) {
    params.append(MONITORING_SELECTED_IDS_QUERY_KEY, id)
  }
}


export function hasActiveMonitoringFilters(filters: MonitoringInstanceFilterState): boolean {
  return (
    filters.group !== null ||
    filters.region !== null ||
    filters.city !== null ||
    filters.provider !== null ||
    filters.lifecycle !== null ||
    filters.runStatus !== null ||
    filters.health !== null ||
    filters.labels.length > 0
  )
}

export function patchSearchParams(
  current: URLSearchParams,
  patch: Record<string, string | null | undefined>,
): URLSearchParams {
  const next = new URLSearchParams(current)
  for (const [key, value] of Object.entries(patch)) {
    if (value == null || value === '') next.delete(key)
    else next.set(key, value)
  }
  return next
}

export function writeMonitoringQuickView(params: URLSearchParams, view: MonitoringInstanceQuickView) {
  params.delete('abnormal')
  params.delete('onboarding')
  if (view === 'all') params.delete('view')
  else params.set('view', view)
}

export function writeMonitoringSearchQuery(params: URLSearchParams, query: string) {
  const trimmed = query.trim()
  if (trimmed) params.set('q', query)
  else params.delete('q')
}

export function writeMonitoringSort(params: URLSearchParams, sort: MonitoringInstanceSortState | null) {
  if (!sort) {
    params.delete('sort')
    params.delete('dir')
    return
  }
  params.set('sort', sort.key)
  if (sort.direction === 'desc') params.set('dir', 'desc')
  else params.delete('dir')
}

export function writeMonitoringFilters(params: URLSearchParams, filters: MonitoringInstanceFilterState) {
  setOrDelete(params, 'group', filters.group)
  setOrDelete(params, 'region', filters.region)
  setOrDelete(params, 'city', filters.city)
  setOrDelete(params, 'provider', filters.provider)
  setOrDelete(params, 'lifecycle', filters.lifecycle)
  setOrDelete(params, 'run_status', filters.runStatus)
  setOrDelete(params, 'health', filters.health)
  if (filters.labels.length > 0) params.set('labels', filters.labels.join(','))
  else params.delete('labels')
}

export function clearMonitoringFilters(params: URLSearchParams) {
  writeMonitoringFilters(params, EMPTY_FILTERS)
}

export function currentMonitoringListHref(search: string): string {
  return search ? `${MONITORING_LIST_PATH}${search}` : MONITORING_LIST_PATH
}

export function resolveMonitoringListHref(state: unknown): string {
  if (typeof state !== 'object' || state === null || !('monitoringListHref' in state)) {
    return MONITORING_LIST_PATH
  }
  const candidate = Reflect.get(state, 'monitoringListHref')
  return typeof candidate === 'string' && MONITORING_LIST_HREF.test(candidate) ? candidate : MONITORING_LIST_PATH
}

export function monitoringListNavigationState(locationState: unknown, href: string): object {
  const base = typeof locationState === 'object' && locationState !== null ? { ...locationState } : {}
  return { ...base, monitoringListHref: href }
}

function normalizeMonitoringSelectedIds(ids: readonly string[]): string[] {
  const seen = new Set<string>()
  const normalized: string[] = []
  for (const rawID of ids) {
    const id = rawID.trim()
    if (!MONITORING_INSTANCE_ID_PATTERN.test(id) || seen.has(id)) continue
    seen.add(id)
    normalized.push(id)
  }
  return normalized
}

function emptyToNull(value: string | null): string | null {
  if (!value) return null
  return value
}

function setOrDelete(params: URLSearchParams, key: string, value: string | null) {
  if (value) params.set(key, value)
  else params.delete(key)
}
