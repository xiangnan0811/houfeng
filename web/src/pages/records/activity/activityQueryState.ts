import type {
  SubjectActivityEventKind,
  SubjectActivityFilter,
  SubjectActivitySourceKind,
  SubjectActivityVersionScope,
  SubjectActivityView,
  RecordSubjectKind,
} from '../../../lib/types'

/**
 * Shareable filter state for a subject activity page. The view lives in the
 * route path; the cursor is page position and is cleared when filters change.
 */
export type SubjectActivityFilters = Omit<SubjectActivityFilter, 'view' | 'cursor'>

export const DEFAULT_SUBJECT_ACTIVITY_FILTERS: SubjectActivityFilters = {}

const MAX_FILTER_VALUES = 32
const MAX_PAGE_SIZE = 100
const DEFAULT_PAGE_SIZE = 50

const SOURCE_KINDS = new Set<SubjectActivitySourceKind>([
  'record_domain',
  'evidence_snapshot',
  'asset_history',
  'monitoring_event',
  'command_audit',
])

const EVENT_KINDS = new Set<SubjectActivityEventKind>([
  'record_created',
  'record_revised',
  'record_restored',
  'record_archived',
  'record_unarchived',
  'record_owner_changed',
  'record_participant_changed',
  'record_follow_up_changed',
  'comment_created',
  'comment_edited',
  'comment_redacted',
  'action_created',
  'action_updated',
  'action_completed',
  'action_cancelled',
  'action_reopened',
  'evidence_captured',
  'asset_fact_changed',
  'monitoring_state_changed',
  'command_executed',
])

const VERSION_SCOPES = new Set<SubjectActivityVersionScope>(['history', 'current'])

const VIEWS = new Set<SubjectActivityView>(['activity', 'records', 'evidence'])

const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,9})?)?(Z|[+-]\d{2}:\d{2})$/

export type SubjectRouteRef = {
  kind: RecordSubjectKind
  sourceId: string
  view: SubjectActivityView
  /** Path prefix without the view segment, e.g. `/vps/vps_001`. */
  basePath: string
}

const ROUTE_KIND_BY_SECTION: Record<string, RecordSubjectKind> = {
  vps: 'vps',
  monitoring: 'monitoring_instance',
  targets: 'target',
}

const VIEW_LABELS: Record<SubjectActivityView, string> = {
  activity: '活动',
  records: '记录',
  evidence: '证据',
}

function vocabulary<Value extends string>(
  allowed: ReadonlySet<Value>,
  raw: readonly string[],
): Value[] | undefined {
  const values: Value[] = []
  for (const item of raw) {
    const trimmed = item.trim() as Value
    if (allowed.has(trimmed) && !values.includes(trimmed) && values.length < MAX_FILTER_VALUES) {
      values.push(trimmed)
    }
  }
  return values.length ? values : undefined
}

function singleVocabulary<Value extends string>(
  allowed: ReadonlySet<Value>,
  raw: string | null,
): Value | undefined {
  const trimmed = (raw ?? '').trim() as Value
  return allowed.has(trimmed) ? trimmed : undefined
}

function instant(raw: string | null): string | undefined {
  const trimmed = (raw ?? '').trim()
  if (!RFC3339.test(trimmed) || Number.isNaN(Date.parse(trimmed))) return undefined
  return trimmed
}

function orderedRange(
  from: string | undefined,
  to: string | undefined,
): [string | undefined, string | undefined] {
  if (from && to && Date.parse(from) >= Date.parse(to)) return [undefined, undefined]
  return [from, to]
}

function pageSize(raw: string | null): number | undefined {
  const trimmed = (raw ?? '').trim()
  if (!/^\d+$/.test(trimmed)) return undefined
  const parsed = Number(trimmed)
  return parsed >= 1 && parsed <= MAX_PAGE_SIZE ? parsed : undefined
}

function optional<Key extends string, Value>(
  key: Key,
  value: Value | undefined,
): Partial<Record<Key, Value>> {
  return value === undefined ? {} : { [key]: value } as Partial<Record<Key, Value>>
}

/**
 * Parses only the allowlisted VPS / monitoring / Target subject routes. Any
 * other section or view shape returns null so the page can 404 rather than
 * invent a subject kind from the URL.
 */
export function parseSubjectActivityRoute(pathname: string): SubjectRouteRef | null {
  const segments = pathname.split('/').filter(Boolean)
  if (segments.length !== 3) return null
  const [section, sourceId, viewRaw] = segments
  const kind = section ? ROUTE_KIND_BY_SECTION[section] : undefined
  const view = singleVocabulary(VIEWS, viewRaw ?? null)
  if (!kind || !sourceId?.trim() || !view) return null
  return {
    kind,
    sourceId: sourceId.trim(),
    view,
    basePath: `/${section}/${sourceId.trim()}`,
  }
}

export function subjectActivityViewLabel(view: SubjectActivityView): string {
  return VIEW_LABELS[view]
}

export function subjectActivityFiltersFromSearchParams(
  searchParams: URLSearchParams,
): SubjectActivityFilters {
  const [from, to] = orderedRange(
    instant(searchParams.get('from')),
    instant(searchParams.get('to')),
  )
  return {
    ...optional('source', vocabulary(SOURCE_KINDS, searchParams.getAll('source'))),
    ...optional('event_kind', vocabulary(EVENT_KINDS, searchParams.getAll('event_kind'))),
    ...optional('from', from),
    ...optional('to', to),
    ...optional('versions', singleVocabulary(VERSION_SCOPES, searchParams.get('versions'))),
    ...optional('limit', pageSize(searchParams.get('limit'))),
  }
}

/**
 * Cursor is page position, not filter state. Changing filters must clear it so
 * a stale watermark from another query is never replayed.
 */
export function subjectActivityCursorFromSearchParams(
  searchParams: URLSearchParams,
): string | undefined {
  const cursor = (searchParams.get('cursor') ?? '').trim()
  return cursor || undefined
}

export function subjectActivityParamsFromState(
  filters: SubjectActivityFilters,
  cursor?: string,
): URLSearchParams {
  const params = new URLSearchParams()
  for (const source of filters.source ?? []) params.append('source', source)
  for (const kind of filters.event_kind ?? []) params.append('event_kind', kind)
  if (filters.from) params.set('from', filters.from)
  if (filters.to) params.set('to', filters.to)
  if (filters.versions && filters.versions !== 'history') {
    params.set('versions', filters.versions)
  }
  if (filters.limit != null && filters.limit !== DEFAULT_PAGE_SIZE) {
    params.set('limit', String(filters.limit))
  }
  const trimmedCursor = cursor?.trim()
  if (trimmedCursor) params.set('cursor', trimmedCursor)
  return params
}

export function subjectActivityFilterKey(filters: SubjectActivityFilters): string {
  return subjectActivityParamsFromState(filters).toString()
}

export function subjectActivityToAPIQuery(
  view: SubjectActivityView,
  filters: SubjectActivityFilters,
  cursor?: string,
): SubjectActivityFilter {
  return {
    ...(view !== 'activity' ? { view } : {}),
    ...filters,
    ...(cursor?.trim() ? { cursor: cursor.trim() } : {}),
  }
}

/** Preselects the subject on `/records/new` and returns the operator here. */
export function subjectNewRecordHref(ref: SubjectRouteRef): string {
  const params = new URLSearchParams()
  params.set('subject', `${ref.kind}:${ref.sourceId}:affected:primary`)
  params.set('return_to', `${ref.basePath}/${ref.view}`)
  return `/records/new?${params}`
}
