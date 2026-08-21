import { describe, expect, it } from 'vitest'

import {
  COMPARISON_URL_VERSION,
  canonicalComparisonURLState,
  comparisonEntryHref,
  comparisonHref,
  comparisonSeriesMetrics,
  comparisonSubjectsFromRecords,
  comparisonSubjectsFromSources,
  encodeComparisonURLState,
  parseComparisonSearchParams,
  parseComparisonURLState,
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
})

function encodeRaw(value: unknown): string {
  return btoa(JSON.stringify(value)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
