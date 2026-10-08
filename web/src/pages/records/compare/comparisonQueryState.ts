import type {
  ComparisonAlignment,
  ComparisonKindRef,
  ComparisonSeries,
  ComparisonSubjectKind,
  ComparisonSubjectRef,
  EvidenceKindName,
  RecordDetail,
  RecordSubjectReference,
} from '../../../lib/types'

export const COMPARISON_URL_VERSION = 'comparison-url/v1'
export const COMPARISON_QUERY_PARAM = 'state'

const SUBJECT_KINDS = new Set<ComparisonSubjectKind>(['vps', 'monitoring_instance', 'target'])
const ALIGNMENTS = new Set<ComparisonAlignment>(['actual_coverage', 'common_overlap'])
const KIND_KEYS = new Map<string, ComparisonKindRef>([
  ['ip_quality.report/v1', { kind: 'ip_quality.report', schema_version: 1 }],
  ['monitoring.host/v1', { kind: 'monitoring.host', schema_version: 1 }],
  ['monitoring.probe/v2', { kind: 'monitoring.probe', schema_version: 2 }],
  ['monitoring.event/v2', { kind: 'monitoring.event', schema_version: 2 }],
  ['subscription.cost/v1', { kind: 'subscription.cost', schema_version: 1 }],
  ['command.audit/v1', { kind: 'command.audit', schema_version: 1 }],
])
const FORBIDDEN_STATE_KEYS = new Set([
  'token',
  'comparison_intent',
  'payload',
  'canonical_payload',
  'title',
  'body_markdown',
  'secret',
  'authorization',
])
const RFC3339_UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$/

export type ComparisonURLFixedItem =
  | { snapshot_id: string }
  | { record_id: string; revision_id: string; snapshot_ids?: string[] }

export type ComparisonURLState = {
  version: typeof COMPARISON_URL_VERSION
  mode: 'candidate' | 'fixed'
  subjects?: ComparisonSubjectRef[]
  items?: ComparisonURLFixedItem[]
  baseline?: number
  alignment?: ComparisonAlignment
  requested_from: string
  requested_to: string
  tolerance_seconds?: number
  bucket_seconds?: number
  kind?: string
  metric?: string
}

export type ComparisonQueryParse =
  | { ok: true; state: ComparisonURLState }
  | { ok: false; reason: 'missing' | 'invalid' | 'unknown_version' }

export function comparisonKindKey(ref: ComparisonKindRef): string {
  return `${ref.kind}/v${ref.schema_version}`
}

export function parseComparisonKindKey(value: string): ComparisonKindRef | null {
  return KIND_KEYS.get(value) ?? null
}

export function defaultComparisonMetric(kind: string): string | undefined {
  if (kind.startsWith('monitoring.host')) return 'cpu_usage_pct'
  if (kind.startsWith('monitoring.probe')) return 'latency_ms'
  return undefined
}

export function encodeComparisonURLState(state: ComparisonURLState): string {
  return bytesToBase64Url(JSON.stringify(canonicalComparisonURLState(state)))
}

export function comparisonSearchParams(state: ComparisonURLState): URLSearchParams {
  const params = new URLSearchParams()
  params.set(COMPARISON_QUERY_PARAM, encodeComparisonURLState(state))
  return params
}

export function comparisonHref(state: ComparisonURLState): string {
  return `/records/compare?${comparisonSearchParams(state).toString()}`
}

export function parseComparisonSearchParams(searchParams: URLSearchParams): ComparisonQueryParse {
  const raw = (searchParams.get(COMPARISON_QUERY_PARAM) ?? '').trim()
  if (!raw) return { ok: false, reason: 'missing' }
  return parseComparisonURLState(raw)
}

export function parseComparisonURLState(encoded: string): ComparisonQueryParse {
  let parsed: unknown
  try {
    parsed = JSON.parse(base64UrlToBytes(encoded))
  } catch {
    return { ok: false, reason: 'invalid' }
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { ok: false, reason: 'invalid' }
  }
  const record = parsed as Record<string, unknown>
  for (const key of Object.keys(record)) {
    if (FORBIDDEN_STATE_KEYS.has(key)) return { ok: false, reason: 'invalid' }
  }
  if (record.version !== COMPARISON_URL_VERSION) {
    return { ok: false, reason: record.version == null ? 'invalid' : 'unknown_version' }
  }
  const requestedFrom = utcInstant(record.requested_from)
  const requestedTo = utcInstant(record.requested_to)
  if (!requestedFrom || !requestedTo || Date.parse(requestedFrom) >= Date.parse(requestedTo)) {
    return { ok: false, reason: 'invalid' }
  }
  const mode = record.mode
  if (mode === 'candidate') {
    const subjects = parseSubjects(record.subjects)
    if (!subjects) return { ok: false, reason: 'invalid' }
    return {
      ok: true,
      state: {
        version: COMPARISON_URL_VERSION,
        mode: 'candidate',
        subjects,
        requested_from: requestedFrom,
        requested_to: requestedTo,
        ...optionalNumber('tolerance_seconds', record.tolerance_seconds),
        ...optionalNumber('bucket_seconds', record.bucket_seconds),
        ...optionalKind(record.kind, record.metric),
      },
    }
  }
  if (mode !== 'fixed') return { ok: false, reason: 'invalid' }
  const items = parseFixedItems(record.items)
  const baseline = integer(record.baseline)
  const alignment = typeof record.alignment === 'string' && ALIGNMENTS.has(record.alignment as ComparisonAlignment)
    ? record.alignment as ComparisonAlignment
    : null
  if (!items || baseline == null || baseline < 0 || baseline >= items.length || !alignment) {
    return { ok: false, reason: 'invalid' }
  }
  return {
    ok: true,
    state: {
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items,
      baseline,
      alignment,
      requested_from: requestedFrom,
      requested_to: requestedTo,
      ...optionalNumber('tolerance_seconds', record.tolerance_seconds),
      ...optionalNumber('bucket_seconds', record.bucket_seconds),
      ...optionalKind(record.kind, record.metric),
    },
  }
}

export function canonicalComparisonURLState(state: ComparisonURLState): Record<string, unknown> {
  const encoded: Record<string, unknown> = {
    version: COMPARISON_URL_VERSION,
    mode: state.mode,
  }
  if (state.mode === 'candidate') {
    encoded.subjects = (state.subjects ?? []).map((subject) => ({ kind: subject.kind, id: subject.id }))
  } else {
    encoded.items = (state.items ?? []).map(canonicalFixedItem)
    encoded.baseline = state.baseline ?? 0
    encoded.alignment = state.alignment ?? 'actual_coverage'
  }
  encoded.requested_from = state.requested_from
  encoded.requested_to = state.requested_to
  if (state.tolerance_seconds != null) encoded.tolerance_seconds = state.tolerance_seconds
  if (state.bucket_seconds != null) encoded.bucket_seconds = state.bucket_seconds
  if (state.kind) encoded.kind = state.kind
  if (state.metric) encoded.metric = state.metric
  return encoded
}

function canonicalFixedItem(item: ComparisonURLFixedItem): Record<string, unknown> {
  if ('snapshot_id' in item && item.snapshot_id) return { snapshot_id: item.snapshot_id }
  const revision = item as { record_id: string; revision_id: string; snapshot_ids?: string[] }
  const encoded: Record<string, unknown> = {
    record_id: revision.record_id,
    revision_id: revision.revision_id,
  }
  if (revision.snapshot_ids?.length) encoded.snapshot_ids = [...revision.snapshot_ids]
  return encoded
}

function parseSubjects(value: unknown): ComparisonSubjectRef[] | null {
  if (!Array.isArray(value) || value.length < 2 || value.length > 6) return null
  const subjects: ComparisonSubjectRef[] = []
  for (const item of value) {
    if (!item || typeof item !== 'object') return null
    const kind = (item as { kind?: unknown }).kind
    const id = typeof (item as { id?: unknown }).id === 'string'
      ? (item as { id: string }).id.trim()
      : ''
    if (typeof kind !== 'string' || !SUBJECT_KINDS.has(kind as ComparisonSubjectKind) || !id) return null
    subjects.push({ kind: kind as ComparisonSubjectKind, id })
  }
  return subjects
}

function parseFixedItems(value: unknown): ComparisonURLFixedItem[] | null {
  if (!Array.isArray(value) || value.length < 1 || value.length > 6) return null
  const items: ComparisonURLFixedItem[] = []
  for (const item of value) {
    if (!item || typeof item !== 'object') return null
    const record = item as Record<string, unknown>
    const snapshotID = typeof record.snapshot_id === 'string' ? record.snapshot_id.trim() : ''
    const recordID = typeof record.record_id === 'string' ? record.record_id.trim() : ''
    const revisionID = typeof record.revision_id === 'string' ? record.revision_id.trim() : ''
    const snapshotIDs = Array.isArray(record.snapshot_ids)
      ? record.snapshot_ids.filter((id): id is string => typeof id === 'string' && id.trim() !== '').map((id) => id.trim())
      : []
    if (snapshotID && !recordID && !revisionID && snapshotIDs.length === 0) {
      items.push({ snapshot_id: snapshotID })
      continue
    }
    if (!snapshotID && recordID && revisionID) {
      items.push(snapshotIDs.length
        ? { record_id: recordID, revision_id: revisionID, snapshot_ids: snapshotIDs }
        : { record_id: recordID, revision_id: revisionID })
      continue
    }
    return null
  }
  return items
}

function utcInstant(value: unknown): string | null {
  if (typeof value !== 'string' || !RFC3339_UTC.test(value)) return null
  const parsed = Date.parse(value)
  if (Number.isNaN(parsed)) return null
  return new Date(parsed).toISOString().replace(/\.\d{3}Z$/, 'Z')
}

function integer(value: unknown): number | null {
  return typeof value === 'number' && Number.isInteger(value) ? value : null
}

function optionalNumber<Key extends string>(
  key: Key,
  value: unknown,
): Partial<Record<Key, number>> {
  const parsed = integer(value)
  return parsed == null || parsed < 0 ? {} : { [key]: parsed } as Partial<Record<Key, number>>
}

function optionalKind(
  kind: unknown,
  metric: unknown,
): { kind?: string; metric?: string } {
  if (typeof kind !== 'string' || !KIND_KEYS.has(kind)) return {}
  const parsedMetric = typeof metric === 'string' && metric.trim() ? metric.trim() : undefined
  return parsedMetric ? { kind, metric: parsedMetric } : { kind }
}

function bytesToBase64Url(value: string): string {
  return btoa(value).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function base64UrlToBytes(value: string): string {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/')
  const pad = padded.length % 4 === 0 ? '' : '='.repeat(4 - (padded.length % 4))
  return atob(padded + pad)
}

export function comparisonKindFromURL(kind: string | undefined): ComparisonKindRef | null {
  return kind ? parseComparisonKindKey(kind) : null
}

export function evidenceKindName(kind: string): EvidenceKindName | null {
  return parseComparisonKindKey(kind)?.kind ?? null
}

export function defaultComparisonWindow(nowMs = Date.now()): {
  requested_from: string
  requested_to: string
} {
  const now = new Date(nowMs)
  const aligned = Date.UTC(
    now.getUTCFullYear(),
    now.getUTCMonth(),
    now.getUTCDate(),
    now.getUTCHours(),
    now.getUTCMinutes(),
  )
  return {
    requested_from: new Date(aligned - 24 * 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z'),
    requested_to: new Date(aligned).toISOString().replace(/\.\d{3}Z$/, 'Z'),
  }
}

export function comparisonSubjectsFromSources(
  sources: readonly Pick<RecordSubjectReference, 'kind' | 'source_id' | 'primary'>[] = [],
): ComparisonSubjectRef[] {
  const ordered = [...sources].sort((left, right) => Number(right.primary) - Number(left.primary))
  const seen = new Set<string>()
  const subjects: ComparisonSubjectRef[] = []
  for (const source of ordered) {
    if (!SUBJECT_KINDS.has(source.kind) || !source.source_id) continue
    const key = `${source.kind}:${source.source_id}`
    if (seen.has(key)) continue
    seen.add(key)
    subjects.push({ kind: source.kind, id: source.source_id })
    if (subjects.length === 6) break
  }
  return subjects
}

export function comparisonSubjectsFromRecords(records: readonly RecordDetail[]): ComparisonSubjectRef[] {
  return comparisonSubjectsFromSources(records.flatMap((record) => record.current?.subjects ?? []))
}

export function seriesForKindAndMetric(
  series: readonly ComparisonSeries[],
  kind?: string,
  metric?: string,
): ComparisonSeries[] {
  if (!isHostOrProbeKind(kind)) return []
  return series.filter((item) => !metric || item.metric_id === metric)
}

export function comparisonSeriesMetrics(series: readonly ComparisonSeries[]): string[] {
  const metrics: string[] = []
  const seen = new Set<string>()
  for (const item of series) {
    const metric = item.metric_id.trim()
    if (!metric || seen.has(metric)) continue
    seen.add(metric)
    metrics.push(metric)
  }
  return metrics
}

export const COMPARISON_FIXED_ITEM_LIMIT = 6

export const COMPARISON_SELECTION_LIMIT_ERROR =
  `最多比较 ${COMPARISON_FIXED_ITEM_LIMIT} 项，超出的选择已拒绝。`

export type ComparisonBasketResult =
  | { ok: false; error: string }
  | { ok: true; changed: boolean; state: ComparisonURLState | null }

/** `snapshot:<id>` or `revision:<record_id>:<revision_id>`. Revision evidence selection is not part of the key. */
export function comparisonFixedItemKey(item: ComparisonURLFixedItem): string {
  if ('snapshot_id' in item) return `snapshot:${item.snapshot_id}`
  return `revision:${item.record_id}:${item.revision_id}`
}

function uniqueTrimmedIDs(ids: readonly string[] | undefined): string[] {
  if (!ids?.length) return []
  const seen = new Set<string>()
  const unique: string[] = []
  for (const raw of ids) {
    const id = raw.trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    unique.push(id)
  }
  return unique
}

function normalizeComparisonFixedItem(item: ComparisonURLFixedItem): ComparisonURLFixedItem | null {
  if ('snapshot_id' in item) {
    const id = item.snapshot_id.trim()
    return id ? { snapshot_id: id } : null
  }
  const recordID = item.record_id.trim()
  const revisionID = item.revision_id.trim()
  if (!recordID || !revisionID) return null
  const snapshotIDs = uniqueTrimmedIDs(item.snapshot_ids)
  return snapshotIDs.length
    ? { record_id: recordID, revision_id: revisionID, snapshot_ids: snapshotIDs }
    : { record_id: recordID, revision_id: revisionID }
}

function dedupeFixedItems(items: readonly ComparisonURLFixedItem[]): ComparisonURLFixedItem[] {
  const ordered: ComparisonURLFixedItem[] = []
  const indexByKey = new Map<string, number>()
  for (const raw of items) {
    const item = normalizeComparisonFixedItem(raw)
    if (!item) continue
    const key = comparisonFixedItemKey(item)
    const existing = indexByKey.get(key)
    if (existing == null) {
      indexByKey.set(key, ordered.length)
      ordered.push(item)
      continue
    }
    if (!('snapshot_id' in item)) ordered[existing] = item
  }
  return ordered
}

function fixedItemsOf(current: ComparisonURLState | null): ComparisonURLFixedItem[] {
  return current?.mode === 'fixed' ? current.items ?? [] : []
}

function withFixedSelection(
  current: ComparisonURLState | null,
  items: ComparisonURLFixedItem[],
  baseline: number,
  nowMs: number,
): ComparisonURLState {
  if (current?.mode === 'fixed') return { ...current, items, baseline }
  const window = current
    ? { requested_from: current.requested_from, requested_to: current.requested_to }
    : defaultComparisonWindow(nowMs)
  return {
    version: COMPARISON_URL_VERSION,
    mode: 'fixed',
    items,
    baseline,
    alignment: current?.alignment ?? 'actual_coverage',
    ...window,
    tolerance_seconds: current?.tolerance_seconds ?? 60,
    ...(current?.bucket_seconds != null ? { bucket_seconds: current.bucket_seconds } : {}),
    ...(current?.kind ? { kind: current.kind } : {}),
    ...(current?.metric ? { metric: current.metric } : {}),
  }
}

function basketResult(
  current: ComparisonURLState | null,
  next: ComparisonURLState | null,
): ComparisonBasketResult {
  if (next == null) {
    return current == null
      ? { ok: true, changed: false, state: null }
      : { ok: true, changed: true, state: null }
  }
  if (
    current
    && JSON.stringify(canonicalComparisonURLState(current)) === JSON.stringify(canonicalComparisonURLState(next))
  ) {
    return { ok: true, changed: false, state: current }
  }
  return { ok: true, changed: true, state: next }
}

function baselineAfterRemoval(length: number, baseline: number, removeIndex: number): number {
  const nextLength = length - 1
  if (nextLength <= 0) return 0
  if (removeIndex < baseline) return baseline - 1
  if (removeIndex > baseline) return baseline
  return Math.min(baseline, nextLength - 1)
}

function baselineAfterReplace(
  previous: readonly ComparisonURLFixedItem[],
  previousBaseline: number,
  next: readonly ComparisonURLFixedItem[],
): number {
  if (next.length === 0) return 0
  const currentBaseline = previous[previousBaseline]
  if (currentBaseline) {
    const key = comparisonFixedItemKey(currentBaseline)
    const kept = next.findIndex((item) => comparisonFixedItemKey(item) === key)
    if (kept >= 0) return kept
  }
  return Math.min(Math.max(previousBaseline, 0), next.length - 1)
}

export function addFixedComparisonItem(
  current: ComparisonURLState | null,
  item: ComparisonURLFixedItem,
  nowMs = Date.now(),
): ComparisonBasketResult {
  const normalized = normalizeComparisonFixedItem(item)
  if (!normalized) return { ok: true, changed: false, state: current }
  const items = fixedItemsOf(current)
  const key = comparisonFixedItemKey(normalized)
  const existing = items.findIndex((entry) => comparisonFixedItemKey(entry) === key)
  if (existing >= 0) {
    if ('snapshot_id' in normalized) return { ok: true, changed: false, state: current }
    const nextItems = items.slice()
    nextItems[existing] = normalized
    const baseline = current?.mode === 'fixed' ? current.baseline ?? 0 : 0
    return basketResult(current, withFixedSelection(current, nextItems, baseline, nowMs))
  }
  if (items.length >= COMPARISON_FIXED_ITEM_LIMIT) {
    return { ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR }
  }
  const baseline = items.length === 0 || current?.mode !== 'fixed' ? 0 : current.baseline ?? 0
  return basketResult(current, withFixedSelection(current, [...items, normalized], baseline, nowMs))
}

export function removeFixedComparisonItem(
  current: ComparisonURLState | null,
  key: string,
): ComparisonBasketResult {
  if (current?.mode !== 'fixed') return { ok: true, changed: false, state: current }
  const items = current.items ?? []
  const index = items.findIndex((item) => comparisonFixedItemKey(item) === key)
  if (index < 0) return { ok: true, changed: false, state: current }
  const nextItems = items.filter((_, itemIndex) => itemIndex !== index)
  if (nextItems.length === 0) return { ok: true, changed: true, state: null }
  const baseline = baselineAfterRemoval(items.length, current.baseline ?? 0, index)
  return basketResult(current, { ...current, items: nextItems, baseline })
}

export function replaceFixedComparisonItems(
  current: ComparisonURLState | null,
  items: readonly ComparisonURLFixedItem[],
  nowMs = Date.now(),
): ComparisonBasketResult {
  const nextItems = dedupeFixedItems(items)
  if (nextItems.length > COMPARISON_FIXED_ITEM_LIMIT) {
    return { ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR }
  }
  if (nextItems.length === 0) {
    return current == null
      ? { ok: true, changed: false, state: null }
      : { ok: true, changed: true, state: null }
  }
  const baseline = baselineAfterReplace(
    fixedItemsOf(current),
    current?.mode === 'fixed' ? current.baseline ?? 0 : 0,
    nextItems,
  )
  return basketResult(current, withFixedSelection(current, nextItems, baseline, nowMs))
}

export function clearFixedComparisonItems(current: ComparisonURLState | null): ComparisonBasketResult {
  if (current == null) return { ok: true, changed: false, state: null }
  return { ok: true, changed: true, state: null }
}

export function confirmComparisonSnapshotItems(
  current: ComparisonURLState | null,
  items: readonly ComparisonURLFixedItem[],
  nowMs = Date.now(),
): ComparisonBasketResult {
  const snapshots: ComparisonURLFixedItem[] = []
  const seen = new Set<string>()
  for (const raw of items) {
    if ('snapshot_id' in raw) {
      const id = raw.snapshot_id.trim()
      if (!id || seen.has(id)) continue
      seen.add(id)
      snapshots.push({ snapshot_id: id })
      continue
    }
    for (const id of uniqueTrimmedIDs(raw.snapshot_ids)) {
      if (seen.has(id)) continue
      seen.add(id)
      snapshots.push({ snapshot_id: id })
    }
  }
  if (snapshots.length > COMPARISON_FIXED_ITEM_LIMIT) {
    return { ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR }
  }
  if (snapshots.length === 0) return { ok: true, changed: false, state: current }
  return replaceFixedComparisonItems(current, snapshots, nowMs)
}

export function comparisonEntryHref(input: {
  items?: ComparisonURLFixedItem[]
  subjects?: ComparisonSubjectRef[]
  now?: number
}): string {
  const window = defaultComparisonWindow(input.now)
  if (input.items?.length) {
    return comparisonHref({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: input.items,
      baseline: 0,
      alignment: 'actual_coverage',
      tolerance_seconds: 60,
      ...window,
    })
  }
  if (input.subjects && input.subjects.length >= 2) {
    return comparisonHref({
      version: COMPARISON_URL_VERSION,
      mode: 'candidate',
      subjects: input.subjects,
      ...window,
    })
  }
  return '/records/compare'
}

export function isHostOrProbeKind(kind: string | undefined): boolean {
  return kind === 'monitoring.host/v1' || kind === 'monitoring.probe/v2'
}
