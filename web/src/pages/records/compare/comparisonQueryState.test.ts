import { describe, expect, it } from 'vitest'

import {
  COMPARISON_SELECTION_LIMIT_ERROR,
  COMPARISON_URL_VERSION,
  addFixedComparisonItem,
  canonicalComparisonURLState,
  clearFixedComparisonItems,
  comparisonEntryHref,
  comparisonFixedItemKey,
  comparisonHref,
  comparisonSeriesMetrics,
  comparisonSubjectsFromRecords,
  comparisonSubjectsFromSources,
  confirmComparisonSnapshotItems,
  encodeComparisonURLState,
  parseComparisonSearchParams,
  parseComparisonURLState,
  removeFixedComparisonItem,
  replaceFixedComparisonItems,
  type ComparisonURLFixedItem,
  type ComparisonURLState,
} from './comparisonQueryState'

const FIXED: ComparisonURLState = {
  version: COMPARISON_URL_VERSION,
  mode: 'fixed',
  items: [
    { snapshot_id: 'evs_a' },
    { record_id: 'rec_b', revision_id: 'rrv_2', snapshot_ids: ['evs_b'] },
  ],
  baseline: 0,
  alignment: 'actual_coverage',
  requested_from: '2026-07-01T00:00:00Z',
  requested_to: '2026-07-02T00:00:00Z',
  tolerance_seconds: 60,
  bucket_seconds: 300,
  kind: 'monitoring.probe/v2',
  metric: 'latency_ms',
}

const CANDIDATE: ComparisonURLState = {
  version: COMPARISON_URL_VERSION,
  mode: 'candidate',
  subjects: [
    { kind: 'vps', id: 'vps_0123456789abcdef' },
    { kind: 'monitoring_instance', id: 'mon_0123456789abcdef' },
  ],
  requested_from: '2026-07-01T00:00:00Z',
  requested_to: '2026-07-02T00:00:00Z',
}

function decodeJSON(encoded: string): Record<string, unknown> {
  const padded = encoded.replace(/-/g, '+').replace(/_/g, '/')
  const pad = padded.length % 4 === 0 ? '' : '='.repeat(4 - (padded.length % 4))
  return JSON.parse(atob(padded + pad)) as Record<string, unknown>
}

describe('comparison query state', () => {
  it('roundtrips 2-6 fixed selections and conditions', () => {
    const encoded = encodeComparisonURLState(FIXED)
    expect(parseComparisonURLState(encoded)).toEqual({ ok: true, state: FIXED })
    expect(parseComparisonSearchParams(new URLSearchParams(`state=${encoded}`))).toEqual({
      ok: true,
      state: FIXED,
    })
    expect(comparisonHref(FIXED)).toBe(`/records/compare?state=${encoded}`)
  })

  it('roundtrips candidate subject mode without inventing fixed IDs', () => {
    expect(parseComparisonURLState(encodeComparisonURLState(CANDIDATE))).toEqual({
      ok: true,
      state: CANDIDATE,
    })
    expect(Object.keys(canonicalComparisonURLState(CANDIDATE))).toEqual([
      'version',
      'mode',
      'subjects',
      'requested_from',
      'requested_to',
    ])
  })

  it('keeps canonical key order and omits payload, title, and tokens', () => {
    const encoded = encodeComparisonURLState({
      ...FIXED,
      items: [
        { snapshot_id: 'evs_a', token: 'secret' } as ComparisonURLState['items'] extends (infer Item)[]
          ? Item
          : never,
        { record_id: 'rec_b', revision_id: 'rrv_2', snapshot_ids: ['evs_b'] },
      ],
    })
    const decoded = decodeJSON(encoded)
    expect(Object.keys(decoded)).toEqual([
      'version',
      'mode',
      'items',
      'baseline',
      'alignment',
      'requested_from',
      'requested_to',
      'tolerance_seconds',
      'bucket_seconds',
      'kind',
      'metric',
    ])
    expect(JSON.stringify(decoded)).not.toMatch(/token|payload|title|secret|comparison_intent/)
    expect(decoded.items).toEqual([
      { snapshot_id: 'evs_a' },
      { record_id: 'rec_b', revision_id: 'rrv_2', snapshot_ids: ['evs_b'] },
    ])
  })

  it('rejects unknown versions, corrupt state, and leaked secrets', () => {
    expect(parseComparisonSearchParams(new URLSearchParams())).toEqual({
      ok: false,
      reason: 'missing',
    })
    expect(parseComparisonURLState('%%%')).toEqual({ ok: false, reason: 'invalid' })
    expect(parseComparisonURLState(encodeRaw({
      ...FIXED,
      version: 'comparison-url/v99',
    }))).toEqual({ ok: false, reason: 'unknown_version' })
    expect(parseComparisonURLState(encodeRaw({
      ...FIXED,
      token: 'cmp1.valid.payload.mac',
    }))).toEqual({ ok: false, reason: 'invalid' })
    expect(parseComparisonURLState(encodeRaw({
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [],
      baseline: 0,
      alignment: 'actual_coverage',
      requested_from: FIXED.requested_from,
      requested_to: FIXED.requested_to,
    }))).toEqual({ ok: false, reason: 'invalid' })
  })

  it('keeps a single seed item in the basket URL without calling it a complete selection', () => {
    const seeded: ComparisonURLState = {
      version: COMPARISON_URL_VERSION,
      mode: 'fixed',
      items: [{ record_id: 'rec_b', revision_id: 'rrv_2' }],
      baseline: 0,
      alignment: 'actual_coverage',
      requested_from: FIXED.requested_from,
      requested_to: FIXED.requested_to,
      tolerance_seconds: 60,
    }
    expect(parseComparisonURLState(encodeComparisonURLState(seeded))).toEqual({
      ok: true,
      state: seeded,
    })
  })

  it('maps the three product entries onto the same /records/compare contract', () => {
    const now = Date.parse('2026-08-20T12:00:00Z')
    expect(comparisonEntryHref({ now })).toBe('/records/compare')

    const fromSearch = comparisonEntryHref({ now })
    expect(fromSearch).toBe('/records/compare')

    const fromRevision = comparisonEntryHref({
      items: [{ record_id: 'rec_b', revision_id: 'rrv_2' }],
      now,
    })
    expect(fromRevision.startsWith('/records/compare?state=')).toBe(true)
    expect(parseComparisonSearchParams(new URL(fromRevision, 'https://example.test').searchParams)).toEqual({
      ok: true,
      state: expect.objectContaining({
        version: COMPARISON_URL_VERSION,
        mode: 'fixed',
        items: [{ record_id: 'rec_b', revision_id: 'rrv_2' }],
      }),
    })

    const fromEvidence = comparisonEntryHref({
      items: [{ snapshot_id: 'evs_a' }],
      now,
    })
    expect(parseComparisonSearchParams(new URL(fromEvidence, 'https://example.test').searchParams)).toEqual({
      ok: true,
      state: expect.objectContaining({
        version: COMPARISON_URL_VERSION,
        mode: 'fixed',
        items: [{ snapshot_id: 'evs_a' }],
      }),
    })

    const fromSubjects = comparisonEntryHref({
      subjects: CANDIDATE.subjects ?? [],
      now,
    })
    expect(parseComparisonSearchParams(new URL(fromSubjects, 'https://example.test').searchParams)).toEqual({
      ok: true,
      state: expect.objectContaining({
        version: COMPARISON_URL_VERSION,
        mode: 'candidate',
        subjects: CANDIDATE.subjects,
      }),
    })
  })

  it('lists unique series metrics in first-seen order', () => {
    expect(comparisonSeriesMetrics([
      { item_index: 0, metric_id: 'cpu_usage_pct', segments: [], unit: '%' },
      { item_index: 1, metric_id: 'cpu_usage_pct', segments: [], unit: '%' },
      { item_index: 0, metric_id: 'mem_used_pct', segments: [], unit: '%' },
    ])).toEqual(['cpu_usage_pct', 'mem_used_pct'])
  })

  it('builds a candidate basket from record subjects and prefers primary ones', () => {
    expect(comparisonSubjectsFromSources([
      { kind: 'vps', source_id: 'vps_b', primary: false },
      { kind: 'vps', source_id: 'vps_a', primary: true },
      { kind: 'vps', source_id: 'vps_a', primary: true },
    ])).toEqual([
      { kind: 'vps', id: 'vps_a' },
      { kind: 'vps', id: 'vps_b' },
    ])
    expect(comparisonSubjectsFromRecords([
      { current: { subjects: [{ kind: 'vps', source_id: 'vps_a', primary: true }] } } as never,
    ])).toEqual([{ kind: 'vps', id: 'vps_a' }])
  })

  it('identifies a snapshot and a revision without including the evidence selection', () => {
    expect(comparisonFixedItemKey({ snapshot_id: 'evs_a' })).toBe('snapshot:evs_a')
    expect(comparisonFixedItemKey({
      record_id: 'rec_b',
      revision_id: 'rrv_2',
      snapshot_ids: ['evs_b', 'evs_c'],
    })).toBe('revision:rec_b:rrv_2')
  })

  it('keeps an explicit revision fixed when subjects are also supplied', () => {
    const now = Date.parse('2026-08-20T12:00:00Z')
    const href = comparisonEntryHref({
      items: [{ record_id: 'rec_b', revision_id: 'rrv_2' }],
      subjects: [
        { kind: 'vps', id: 'vps_0123456789abcdef' },
        { kind: 'monitoring_instance', id: 'mi_0123456789abcdef' },
      ],
      now,
    })
    const parsed = parseComparisonSearchParams(new URL(href, 'https://example.test').searchParams)
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    expect(parsed.state.mode).toBe('fixed')
    expect(parsed.state.items).toEqual([{ record_id: 'rec_b', revision_id: 'rrv_2' }])
    expect(parsed.state.subjects).toBeUndefined()
    expect(parsed.state.baseline).toBe(0)
  })

  it('keeps two revisions and two snapshots, and replaces snapshot selection on the same revision', () => {
    const now = Date.parse('2026-08-20T12:00:00Z')
    const first = addFixedComparisonItem(null, {
      record_id: 'rec_same',
      revision_id: 'rrv_1',
      snapshot_ids: ['evs_a', ' evs_a ', 'evs_b'],
    }, now)
    const second = addFixedComparisonItem(first.ok ? first.state : null, {
      record_id: 'rec_same',
      revision_id: 'rrv_2',
      snapshot_ids: ['evs_c'],
    }, now)
    expect(second.ok && second.state?.items).toEqual([
      { record_id: 'rec_same', revision_id: 'rrv_1', snapshot_ids: ['evs_a', 'evs_b'] },
      { record_id: 'rec_same', revision_id: 'rrv_2', snapshot_ids: ['evs_c'] },
    ])
    const replaced = addFixedComparisonItem(second.ok ? second.state : null, {
      record_id: 'rec_same',
      revision_id: 'rrv_1',
      snapshot_ids: ['evs_d'],
    }, now)
    expect(replaced.ok && replaced.changed).toBe(true)
    expect(replaced.ok && replaced.state?.items).toEqual([
      { record_id: 'rec_same', revision_id: 'rrv_1', snapshot_ids: ['evs_d'] },
      { record_id: 'rec_same', revision_id: 'rrv_2', snapshot_ids: ['evs_c'] },
    ])
    const clearedSelection = addFixedComparisonItem(replaced.ok ? replaced.state : null, {
      record_id: 'rec_same',
      revision_id: 'rrv_1',
    }, now)
    expect(clearedSelection.ok && clearedSelection.state?.items?.[0]).toEqual({
      record_id: 'rec_same',
      revision_id: 'rrv_1',
    })

    const seeded = addFixedComparisonItem(null, { snapshot_id: ' evs_a ' }, now)
    const snapshots = addFixedComparisonItem(seeded.ok ? seeded.state : null, { snapshot_id: 'evs_b' }, now)
    const duplicate = addFixedComparisonItem(snapshots.ok ? snapshots.state : null, { snapshot_id: 'evs_a' }, now)
    expect(snapshots.ok && snapshots.state?.items).toEqual([
      { snapshot_id: 'evs_a' },
      { snapshot_id: 'evs_b' },
    ])
    expect(duplicate).toMatchObject({ ok: true, changed: false })
    expect(duplicate.ok && duplicate.state?.items).toHaveLength(2)
  })

  it('rejects a seventh distinct object without truncating or mutating the basket', () => {
    const six = fixedWith(Array.from({ length: 6 }, (_, index) => ({ snapshot_id: `evs_${index}` })))
    const seventh = addFixedComparisonItem(six, { snapshot_id: 'evs_6' })
    expect(seventh).toEqual({ ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR })
    expect(six.items).toHaveLength(6)

    const replaced = replaceFixedComparisonItems(six, [
      ...six.items ?? [],
      { snapshot_id: 'evs_6' },
    ])
    expect(replaced).toEqual({ ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR })
    expect(six.items).toHaveLength(6)

    const deduped = replaceFixedComparisonItems(null, [
      ...six.items ?? [],
      { snapshot_id: 'evs_0' },
    ], Date.parse('2026-08-20T12:00:00Z'))
    expect(deduped.ok && deduped.state?.items).toHaveLength(6)
    expect(addFixedComparisonItem(six, { snapshot_id: 'evs_0' })).toMatchObject({ ok: true, changed: false })

    const full = fixedWith([
      { record_id: 'rec_same', revision_id: 'rrv_1', snapshot_ids: ['evs_a'] },
      ...Array.from({ length: 5 }, (_, index) => ({ snapshot_id: `evs_${index}` })),
    ])
    expect(addFixedComparisonItem(full, {
      record_id: 'rec_other',
      revision_id: 'rrv_9',
    })).toEqual({ ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR })
    const updated = addFixedComparisonItem(full, {
      record_id: 'rec_same',
      revision_id: 'rrv_1',
      snapshot_ids: ['evs_next'],
    })
    expect(updated.ok && updated.state?.items).toHaveLength(6)
    expect(updated.ok && updated.state?.items?.[0]).toEqual({
      record_id: 'rec_same',
      revision_id: 'rrv_1',
      snapshot_ids: ['evs_next'],
    })
  })

  it('moves the baseline to the next item, otherwise the last, and clears the last object', () => {
    const basket = fixedWith([
      { snapshot_id: 'evs_a' },
      { snapshot_id: 'evs_b' },
      { snapshot_id: 'evs_c' },
    ])
    const removeBaseline = removeFixedComparisonItem(basket, 'snapshot:evs_a')
    expect(removeBaseline.ok && removeBaseline.state?.items).toEqual([
      { snapshot_id: 'evs_b' },
      { snapshot_id: 'evs_c' },
    ])
    expect(removeBaseline.ok && removeBaseline.state?.baseline).toBe(0)
    expect(removeBaseline.ok && removeBaseline.state?.kind).toBe('monitoring.probe/v2')

    const removeLastBaseline = removeFixedComparisonItem({ ...basket, baseline: 2 }, 'snapshot:evs_c')
    expect(removeLastBaseline.ok && removeLastBaseline.state?.baseline).toBe(1)
    expect(removeLastBaseline.ok && removeLastBaseline.state?.items).toEqual([
      { snapshot_id: 'evs_a' },
      { snapshot_id: 'evs_b' },
    ])

    const removeBefore = removeFixedComparisonItem({ ...basket, baseline: 1 }, 'snapshot:evs_a')
    expect(removeBefore.ok && removeBefore.state?.baseline).toBe(0)
    expect(removeBefore.ok && removeBefore.state?.items?.[0]).toEqual({ snapshot_id: 'evs_b' })

    const removeAfter = removeFixedComparisonItem({ ...basket, baseline: 1 }, 'snapshot:evs_c')
    expect(removeAfter.ok && removeAfter.state?.baseline).toBe(1)

    const reordered = replaceFixedComparisonItems({ ...basket, baseline: 2 }, [
      { snapshot_id: 'evs_c' },
      { snapshot_id: 'evs_a' },
    ])
    expect(reordered.ok && reordered.state?.baseline).toBe(0)
    expect(reordered.ok && reordered.state?.items).toEqual([
      { snapshot_id: 'evs_c' },
      { snapshot_id: 'evs_a' },
    ])

    const emptied = removeFixedComparisonItem(
      fixedWith([{ snapshot_id: 'evs_a' }]),
      'snapshot:evs_a',
    )
    expect(emptied).toEqual({ ok: true, changed: true, state: null })
    expect(clearFixedComparisonItems(basket)).toEqual({ ok: true, changed: true, state: null })
    expect(clearFixedComparisonItems(null)).toEqual({ ok: true, changed: false, state: null })
  })

  it('confirms selected snapshot ids and does not promote a revision id', () => {
    const now = Date.parse('2026-08-20T12:00:00Z')
    const confirmed = confirmComparisonSnapshotItems(CANDIDATE, [
      { record_id: 'rec_a', revision_id: 'rrv_ignored', snapshot_ids: ['evs_a', 'evs_a'] },
      { snapshot_id: 'evs_b' },
      { record_id: 'rec_bare', revision_id: 'rrv_bare' },
    ], now)
    expect(confirmed.ok && confirmed.state?.mode).toBe('fixed')
    expect(confirmed.ok && confirmed.state?.items).toEqual([
      { snapshot_id: 'evs_a' },
      { snapshot_id: 'evs_b' },
    ])

    const bare = confirmComparisonSnapshotItems(CANDIDATE, [
      { record_id: 'rec_a', revision_id: 'rrv_a' },
      { record_id: 'rec_b', revision_id: 'rrv_b' },
    ], now)
    expect(bare).toEqual({ ok: true, changed: false, state: CANDIDATE })

    const tooMany = confirmComparisonSnapshotItems(CANDIDATE, Array.from(
      { length: 7 },
      (_, index) => ({ snapshot_id: `evs_${index}` }),
    ), now)
    expect(tooMany).toEqual({ ok: false, error: COMPARISON_SELECTION_LIMIT_ERROR })
  })
})

function fixedWith(items: ComparisonURLFixedItem[], baseline = 0): ComparisonURLState {
  return {
    ...FIXED,
    items,
    baseline,
  }
}

function encodeRaw(value: unknown): string {
  return btoa(JSON.stringify(value)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
