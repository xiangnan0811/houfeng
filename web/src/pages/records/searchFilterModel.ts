import type {
  RecordActionState,
  RecordBusinessStatus,
  RecordFollowUpState,
  RecordLifecycle,
  RecordSearchFilter,
  RecordSearchSubjectFilter,
  RecordSort,
  RecordStatusGroup,
  RecordSubjectKind,
  RecordSubjectPlacement,
  RecordRelationRole,
  RecordType,
} from '../../lib/types'

/**
 * Filter state for the records search page. It mirrors the query the server
 * accepts, minus the cursor: the cursor is page position rather than filter
 * state, so it never enters the shareable URL.
 */
export type RecordSearchFilters = Omit<RecordSearchFilter, 'cursor'>

export const DEFAULT_RECORD_SEARCH_FILTERS: RecordSearchFilters = {}

/** Mirrors recordsearch.MaxQueryFilterValues; a longer list is rejected server-side. */
const MAX_FILTER_VALUES = 32
/** Mirrors recordsearch.MaxPageSize. */
const MAX_PAGE_SIZE = 100

const RECORD_TYPES = new Set<RecordType>([
  'troubleshooting', 'maintenance', 'migration',
  'provider_communication', 'billing', 'important_finding', 'note',
])
const BUSINESS_STATUSES = new Set<RecordBusinessStatus>([
  'pending_investigation', 'investigating', 'verifying', 'resolved', 'closed',
  'cancelled', 'planned', 'executing', 'completed', 'pending_contact',
  'waiting_provider', 'waiting_internal', 'pending_review', 'processing',
])
const STATUS_GROUPS = new Set<RecordStatusGroup>([
  'pending', 'in_progress', 'waiting', 'verification', 'completed', 'cancelled',
])
const LIFECYCLES = new Set<RecordLifecycle>(['active', 'archived'])
const SUBJECT_KINDS = new Set<RecordSubjectKind>(['vps', 'monitoring_instance', 'target'])
const RELATION_ROLES = new Set<RecordRelationRole>(['affected', 'context', 'evidence_source'])
const SUBJECT_PLACEMENTS = new Set<RecordSubjectPlacement>(['primary', 'related'])
const FOLLOW_UP_STATES = new Set<RecordFollowUpState>(['none', 'scheduled', 'overdue'])
const ACTION_STATES = new Set<RecordActionState>(['none', 'open', 'overdue'])
const SORTS = new Set<RecordSort>(['updated_at_desc', 'updated_at_asc'])

/**
 * The server owns the vocabularies, so anything outside them is dropped here
 * rather than forwarded. A typo in a shared link then narrows the result set
 * instead of turning the page into an error.
 */
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

function freeTextValues(raw: readonly string[]): string[] | undefined {
  const values: string[] = []
  for (const item of raw) {
    const trimmed = item.trim()
    if (trimmed && !values.includes(trimmed) && values.length < MAX_FILTER_VALUES) {
      values.push(trimmed)
    }
  }
  return values.length ? values : undefined
}

function instant(raw: string | null): string | undefined {
  const trimmed = (raw ?? '').trim()
  if (!trimmed || Number.isNaN(Date.parse(trimmed))) return undefined
  return trimmed
}

/** A range whose end precedes its start can never match, so neither bound is kept. */
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

function subjectFilters(raw: readonly string[]): RecordSearchSubjectFilter[] | undefined {
  const filters: RecordSearchSubjectFilter[] = []
  for (const item of raw) {
    const segments = item.split(':')
    if (segments.length > 4 || filters.length >= MAX_FILTER_VALUES) continue
    const [rawKind = '', rawSource = '', rawRole = '', rawPlacement = ''] = segments
    const kind = singleVocabulary(SUBJECT_KINDS, rawKind)
    const role = singleVocabulary(RELATION_ROLES, rawRole)
    const placement = singleVocabulary(SUBJECT_PLACEMENTS, rawPlacement)
    const sourceID = rawSource.trim()
    // An unknown token in a slot is a different question, not a wider one, so the
    // whole filter is discarded rather than silently widened to "any".
    if ((rawKind.trim() && !kind) || (rawRole.trim() && !role)
      || (rawPlacement.trim() && !placement)) {
      continue
    }
    if (!kind && !sourceID && !role && !placement) continue
    filters.push({
      ...(kind ? { kind } : {}),
      ...(sourceID ? { source_id: sourceID } : {}),
      ...(role ? { role } : {}),
      ...(placement ? { placement } : {}),
    })
  }
  return filters.length ? filters : undefined
}

/**
 * The server reads a subject by position, so every segment keeps its slot even
 * when empty.
 */
function encodeSubject(subject: RecordSearchSubjectFilter): string {
  return [
    subject.kind ?? '', subject.source_id ?? '', subject.role ?? '', subject.placement ?? '',
  ].join(':')
}

export function recordSearchFiltersFromSearchParams(
  searchParams: URLSearchParams,
): RecordSearchFilters {
  const text = (searchParams.get('q') ?? '').trim()
  const [occurredFrom, occurredTo] = orderedRange(
    instant(searchParams.get('occurred_from')), instant(searchParams.get('occurred_to')),
  )
  const [updatedFrom, updatedTo] = orderedRange(
    instant(searchParams.get('updated_from')), instant(searchParams.get('updated_to')),
  )
  const filters: RecordSearchFilters = {
    ...(text ? { q: text } : {}),
    ...optional('type', vocabulary(RECORD_TYPES, searchParams.getAll('type'))),
    ...optional('status', vocabulary(BUSINESS_STATUSES, searchParams.getAll('status'))),
    ...optional('status_group', vocabulary(STATUS_GROUPS, searchParams.getAll('status_group'))),
    ...optional('lifecycle', vocabulary(LIFECYCLES, searchParams.getAll('lifecycle'))),
    ...optional('owner', freeTextValues(searchParams.getAll('owner'))),
    ...optional('participant', freeTextValues(searchParams.getAll('participant'))),
    ...optional('tag', freeTextValues(searchParams.getAll('tag'))),
    ...optional('subject', subjectFilters(searchParams.getAll('subject'))),
    ...optional('follow_up', singleVocabulary(FOLLOW_UP_STATES, searchParams.get('follow_up'))),
    ...optional('action', singleVocabulary(ACTION_STATES, searchParams.get('action'))),
    ...optional('occurred_from', occurredFrom),
    ...optional('occurred_to', occurredTo),
    ...optional('updated_from', updatedFrom),
    ...optional('updated_to', updatedTo),
    ...optional('sort', singleVocabulary(SORTS, searchParams.get('sort'))),
    ...optional('limit', pageSize(searchParams.get('limit'))),
  }
  return filters
}

function optional<Key extends string, Value>(
  key: Key,
  value: Value | undefined,
): Record<Key, Value> | Record<string, never> {
  return value === undefined ? {} : ({ [key]: value } as Record<Key, Value>)
}

export function recordSearchParamsFromFilters(filters: RecordSearchFilters): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.q) params.set('q', filters.q)
  for (const [key, values] of [
    ['type', filters.type], ['status', filters.status], ['status_group', filters.status_group],
    ['lifecycle', filters.lifecycle], ['owner', filters.owner],
    ['participant', filters.participant], ['tag', filters.tag],
  ] as const) {
    for (const value of values ?? []) params.append(key, value)
  }
  for (const subject of filters.subject ?? []) params.append('subject', encodeSubject(subject))
  if (filters.follow_up) params.set('follow_up', filters.follow_up)
  if (filters.action) params.set('action', filters.action)
  if (filters.occurred_from) params.set('occurred_from', filters.occurred_from)
  if (filters.occurred_to) params.set('occurred_to', filters.occurred_to)
  if (filters.updated_from) params.set('updated_from', filters.updated_from)
  if (filters.updated_to) params.set('updated_to', filters.updated_to)
  if (filters.sort) params.set('sort', filters.sort)
  if (filters.limit) params.set('limit', String(filters.limit))
  return params
}

/**
 * A stable identity for one filter set, used to tell a real filter change from a
 * re-render so the page does not refetch on every keystroke of unrelated state.
 */
export function recordSearchFilterKey(filters: RecordSearchFilters): string {
  const params = recordSearchParamsFromFilters(filters)
  params.sort()
  return params.toString()
}

export function recordSearchToAPIQuery(
  filters: RecordSearchFilters,
  cursor?: string,
): RecordSearchFilter {
  return {
    ...recordSearchFiltersFromSearchParams(recordSearchParamsFromFilters(filters)),
    ...optional('cursor', cursor),
  }
}
