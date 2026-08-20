import { describe, expect, it } from 'vitest'

import {
  DEFAULT_SUBJECT_ACTIVITY_FILTERS,
  parseSubjectActivityRoute,
  subjectActivityCursorFromSearchParams,
  subjectActivityFilterKey,
  subjectActivityFiltersFromSearchParams,
  subjectActivityParamsFromState,
  subjectActivityToAPIQuery,
  subjectNewRecordHref,
} from './activityQueryState'

function parse(search: string) {
  return subjectActivityFiltersFromSearchParams(new URLSearchParams(search))
}

describe('subject activity query state', () => {
  it('parses only allowlisted VPS / monitoring / Target routes', () => {
    expect(parseSubjectActivityRoute('/vps/vps_001/activity')).toEqual({
      kind: 'vps',
      sourceId: 'vps_001',
      view: 'activity',
      basePath: '/vps/vps_001',
    })
    expect(parseSubjectActivityRoute('/monitoring/mi_001/records')).toEqual({
      kind: 'monitoring_instance',
      sourceId: 'mi_001',
      view: 'records',
      basePath: '/monitoring/mi_001',
    })
    expect(parseSubjectActivityRoute('/targets/tg_001/evidence')).toEqual({
      kind: 'target',
      sourceId: 'tg_001',
      view: 'evidence',
      basePath: '/targets/tg_001',
    })
    expect(parseSubjectActivityRoute('/providers/prv_001/activity')).toBeNull()
    expect(parseSubjectActivityRoute('/vps/vps_001/timeline')).toBeNull()
    expect(parseSubjectActivityRoute('/vps/vps_001')).toBeNull()
  })

  it('reads filters and drops values outside closed vocabularies', () => {
    expect(parse(
      'source=record_domain&source=command_audit&source=nope'
      + '&event_kind=record_revised&event_kind=guess'
      + '&from=2026-08-01T00:00:00Z&to=2026-08-08T00:00:00Z'
      + '&versions=current&limit=25',
    )).toEqual({
      source: ['record_domain', 'command_audit'],
      event_kind: ['record_revised'],
      from: '2026-08-01T00:00:00Z',
      to: '2026-08-08T00:00:00Z',
      versions: 'current',
      limit: 25,
    })
    expect(parse('versions=latest&limit=101&from=2026-08-01')).toEqual(
      DEFAULT_SUBJECT_ACTIVITY_FILTERS,
    )
  })

  it('keeps cursor out of filter state and clears it when filters encode', () => {
    const params = new URLSearchParams(
      'source=record_domain&cursor=opaque-token-page-2',
    )
    expect(subjectActivityFiltersFromSearchParams(params)).toEqual({
      source: ['record_domain'],
    })
    expect(subjectActivityCursorFromSearchParams(params)).toBe('opaque-token-page-2')
    expect(subjectActivityParamsFromState({ source: ['record_domain'] }).has('cursor'))
      .toBe(false)
  })

  it('omits defaults when encoding shareable params and API queries', () => {
    expect(subjectActivityParamsFromState({
      versions: 'history',
      limit: 50,
    }).toString()).toBe('')
    expect(subjectActivityToAPIQuery('activity', {}, undefined)).toEqual({})
    expect(subjectActivityToAPIQuery('records', {
      source: ['evidence_snapshot', 'record_domain'],
      limit: 25,
    }, '  next-opaque  ')).toEqual({
      view: 'records',
      source: ['evidence_snapshot', 'record_domain'],
      limit: 25,
      cursor: 'next-opaque',
    })
  })

  it('treats opaque cursors as opaque bytes — never compares or decodes them', () => {
    const a = 'opaque-A-watermark'
    const b = 'opaque-B-watermark'
    expect(a < b || a > b).toBe(true)
    // Codec only stores/returns the string; equality is identity of the string
    // the server gave us, not a decoded watermark comparison.
    expect(subjectActivityCursorFromSearchParams(new URLSearchParams(`cursor=${a}`)))
      .toBe(a)
    expect(subjectActivityFilterKey({ source: ['record_domain'] }))
      .not.toContain('opaque')
  })

  it('builds a VPS-local new-record link with return URL', () => {
    expect(subjectNewRecordHref({
      kind: 'vps',
      sourceId: 'vps_001',
      view: 'activity',
      basePath: '/vps/vps_001',
    })).toBe(
      '/records/new?subject=vps%3Avps_001%3Aaffected%3Aprimary&return_to=%2Fvps%2Fvps_001%2Factivity',
    )
  })
})
