import type {
  RecordActionState,
  RecordBusinessStatus,
  RecordFollowUpState,
  RecordLifecycle,
  RecordSearchFilter,
  RecordSearchSubjectFilter,
  RecordStatusGroup,
  RecordType,
} from '../../lib/types'
import {
  labelVocabulary,
  RECORD_ACTION_LABELS,
  RECORD_FOLLOW_UP_LABELS,
  RECORD_LIFECYCLE_LABELS,
  RECORD_RELATION_ROLE_LABELS,
  RECORD_SORT_LABELS,
  RECORD_STATUS_GROUP_LABELS,
  RECORD_SUBJECT_KIND_LABELS,
  RECORD_SUBJECT_PLACEMENT_LABELS,
  RECORD_TYPE_LABELS,
} from './recordLabels'
import { BUSINESS_STATUS_LABELS } from './recordWorkspaceModel'

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

const RECORD_TYPES = labelVocabulary(RECORD_TYPE_LABELS)
const BUSINESS_STATUSES = labelVocabulary(BUSINESS_STATUS_LABELS)
const STATUS_GROUPS = labelVocabulary(RECORD_STATUS_GROUP_LABELS)
const LIFECYCLES = labelVocabulary(RECORD_LIFECYCLE_LABELS)
const SUBJECT_KINDS = labelVocabulary(RECORD_SUBJECT_KIND_LABELS)
const RELATION_ROLES = labelVocabulary(RECORD_RELATION_ROLE_LABELS)
const SUBJECT_PLACEMENTS = labelVocabulary(RECORD_SUBJECT_PLACEMENT_LABELS)
const FOLLOW_UP_STATES = labelVocabulary(RECORD_FOLLOW_UP_LABELS)
const ACTION_STATES = labelVocabulary(RECORD_ACTION_LABELS)
const SORTS = labelVocabulary(RECORD_SORT_LABELS)

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

/**
 * The server parses these as RFC3339, so a zone is mandatory. A zone-less local
 * datetime parses happily in the browser but would come back as a 400, so it is
 * dropped here instead.
 */
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,9})?)?(Z|[+-]\d{2}:\d{2})$/

function instant(raw: string | null): string | undefined {
  const trimmed = (raw ?? '').trim()
  if (!RFC3339.test(trimmed) || Number.isNaN(Date.parse(trimmed))) return undefined
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

/** One removable summary of an active filter value. */
export type RecordSearchFilterChip = {
  key: string
  label: string
  /** The filter set with this one value removed. */
  next: RecordSearchFilters
}

/**
 * Sets a filter, or clears it when the value is absent. An inactive filter has
 * to be a missing key rather than a key holding `undefined`, both because
 * `exactOptionalPropertyTypes` distinguishes the two and because the URL codec
 * treats a present key as a real constraint.
 */
export function withFilter<Field extends keyof RecordSearchFilters>(
  filters: RecordSearchFilters,
  field: Field,
  value: RecordSearchFilters[Field] | undefined,
): RecordSearchFilters {
  const next: Record<string, unknown> = { ...filters }
  if (value === undefined) delete next[field]
  else next[field] = value
  return next as RecordSearchFilters
}

function withoutIndex<Value>(values: readonly Value[], index: number): Value[] | undefined {
  const remaining = values.filter((_, position) => position !== index)
  return remaining.length ? remaining : undefined
}

function chipsForList(
  filters: RecordSearchFilters,
  field: 'type' | 'status' | 'status_group' | 'lifecycle' | 'owner' | 'participant' | 'tag',
  caption: string,
  describe: (value: string) => string,
): RecordSearchFilterChip[] {
  const values: readonly string[] = filters[field] ?? []
  return values.map((value, index) => ({
    key: `${field}:${value}`,
    label: `${caption}: ${describe(value)}`,
    next: withFilter(filters, field, withoutIndex(values, index)),
  }))
}

/**
 * Every active filter as a removable chip. Filters with no dedicated control —
 * subjects, which arrive from a link on a VPS or monitoring page — are still
 * visible and removable here, so a shared link never applies a narrowing the
 * reader cannot see.
 */
export function recordSearchFilterChips(filters: RecordSearchFilters): RecordSearchFilterChip[] {
  const chips: RecordSearchFilterChip[] = [
    ...chipsForList(filters, 'type', '类型', (value) => RECORD_TYPE_LABELS[value as RecordType]),
    ...chipsForList(filters, 'status', '状态',
      (value) => BUSINESS_STATUS_LABELS[value as RecordBusinessStatus]),
    ...chipsForList(filters, 'status_group', '状态分组',
      (value) => RECORD_STATUS_GROUP_LABELS[value as RecordStatusGroup]),
    ...chipsForList(filters, 'lifecycle', '生命周期',
      (value) => RECORD_LIFECYCLE_LABELS[value as RecordLifecycle]),
    ...chipsForList(filters, 'owner', '负责人', (value) => value),
    ...chipsForList(filters, 'participant', '参与人', (value) => value),
    ...chipsForList(filters, 'tag', '标签', (value) => value),
  ]
  const subjects = filters.subject ?? []
  for (const [index, subject] of subjects.entries()) {
    const parts = [
      subject.kind ? RECORD_SUBJECT_KIND_LABELS[subject.kind] : '任意类型',
      subject.source_id ?? '任意对象',
      subject.role ? RECORD_RELATION_ROLE_LABELS[subject.role] : '任意角色',
      subject.placement ? RECORD_SUBJECT_PLACEMENT_LABELS[subject.placement] : '任意位置',
    ]
    chips.push({
      key: `subject:${encodeSubject(subject)}`,
      label: `对象: ${parts.join(' / ')}`,
      next: withFilter(filters, 'subject', withoutIndex(subjects, index)),
    })
  }
  for (const [field, caption, describe] of [
    ['follow_up', '跟进', (value: string) => RECORD_FOLLOW_UP_LABELS[value as RecordFollowUpState]],
    ['action', '待办', (value: string) => RECORD_ACTION_LABELS[value as RecordActionState]],
    ['occurred_from', '发生于之后', (value: string) => value],
    ['occurred_to', '发生于之前', (value: string) => value],
    ['updated_from', '更新于之后', (value: string) => value],
    ['updated_to', '更新于之前', (value: string) => value],
  ] as const) {
    const value = filters[field]
    if (!value) continue
    chips.push({
      key: field,
      label: `${caption}: ${describe(value)}`,
      next: withFilter(filters, field, undefined),
    })
  }
  return chips
}

/** Free-text list filters are edited as one comma-separated field. */
export function parseFilterValueList(text: string): string[] | undefined {
  return freeTextValues(text.split(/[,，\s]+/))
}

export function formatFilterValueList(values: readonly string[] | undefined): string {
  return (values ?? []).join(', ')
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
