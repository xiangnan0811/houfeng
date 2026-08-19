import { describe, expect, it } from 'vitest'

import {
  DEFAULT_RECORD_SEARCH_FILTERS,
  formatFilterValueList,
  parseFilterValueList,
  recordSearchFilterChips,
  recordSearchFilterKey,
  recordSearchFiltersFromSearchParams,
  recordSearchParamsFromFilters,
  recordSearchToAPIQuery,
} from './searchFilterModel'

function parse(search: string) {
  return recordSearchFiltersFromSearchParams(new URLSearchParams(search))
}

describe('record search filter model', () => {
  it('reads every filter the search endpoint accepts', () => {
    expect(parse(
      'q=disk+io&type=troubleshooting&type=migration&status=resolved'
      + '&status_group=in_progress&lifecycle=archived&owner=usr_a&participant=usr_b'
      + '&tag=ops&tag=disk&subject=vps%3Avps_alpha%3Aaffected%3Aprimary'
      + '&follow_up=overdue&action=open'
      + '&occurred_from=2026-08-01T00%3A00%3A00Z&occurred_to=2026-08-08T00%3A00%3A00Z'
      + '&updated_from=2026-08-02T00%3A00%3A00Z&updated_to=2026-08-09T00%3A00%3A00Z'
      + '&sort=updated_at_asc&limit=25',
    )).toEqual({
      q: 'disk io',
      type: ['troubleshooting', 'migration'],
      status: ['resolved'],
      status_group: ['in_progress'],
      lifecycle: ['archived'],
      owner: ['usr_a'],
      participant: ['usr_b'],
      tag: ['ops', 'disk'],
      subject: [{ kind: 'vps', source_id: 'vps_alpha', role: 'affected', placement: 'primary' }],
      follow_up: 'overdue',
      action: 'open',
      occurred_from: '2026-08-01T00:00:00Z',
      occurred_to: '2026-08-08T00:00:00Z',
      updated_from: '2026-08-02T00:00:00Z',
      updated_to: '2026-08-09T00:00:00Z',
      sort: 'updated_at_asc',
      limit: 25,
    })
  })

  it('drops values outside the closed vocabularies rather than forwarding them', () => {
    // A filter the server would reject must not reach it, or a typo in a shared
    // link turns the whole page into an error instead of a usable result.
    expect(parse(
      'type=troubleshooting&type=not_a_type&status=nope&status_group=nope'
      + '&lifecycle=deleted&follow_up=maybe&action=maybe&sort=relevance',
    )).toEqual({
      ...DEFAULT_RECORD_SEARCH_FILTERS,
      type: ['troubleshooting'],
    })
  })

  it('keeps a subject filter usable when only some segments are given', () => {
    expect(parse('subject=vps%3A%3Acontext&subject=%3A%3A%3Aprimary').subject).toEqual([
      { kind: 'vps', role: 'context' },
      { placement: 'primary' },
    ])
  })

  it('discards a subject filter with no usable segment or an unknown vocabulary', () => {
    expect(parse('subject=&subject=%3A%3A%3A&subject=nope%3A%3A%3A&subject=vps%3Aa%3Ab%3Ac%3Ad'))
      .toEqual(DEFAULT_RECORD_SEARCH_FILTERS)
  })

  it('ignores a limit that is not a whole number inside the server bound', () => {
    for (const raw of ['0', '-5', '101', '25.5', 'many', '  ']) {
      expect(parse(`limit=${encodeURIComponent(raw)}`).limit).toBeUndefined()
    }
    expect(parse('limit=100').limit).toBe(100)
  })

  it('ignores a timestamp that is not an instant, and an inverted range', () => {
    expect(parse('occurred_from=yesterday').occurred_from).toBeUndefined()
    // Zone-less: the browser would parse it as local time, the server as a 400.
    expect(parse('occurred_from=2026-08-01T00%3A00').occurred_from).toBeUndefined()
    expect(parse('occurred_from=2026-08-01T00%3A00%3A00%2B08%3A00').occurred_from)
      .toBe('2026-08-01T00:00:00+08:00')
    expect(parse(
      'updated_from=2026-08-09T00%3A00%3A00Z&updated_to=2026-08-02T00%3A00%3A00Z',
    )).toEqual(DEFAULT_RECORD_SEARCH_FILTERS)
  })

  it('bounds repeated filters to the value count the server accepts', () => {
    const tags = Array.from({ length: 40 }, (_, index) => `tag=t${index}`).join('&')
    expect(parse(tags).tag).toHaveLength(32)
  })

  it('round-trips through the URL and omits everything left at its default', () => {
    const filters = parse(
      'q=disk&type=migration&tag=ops&subject=vps%3A%3Acontext&sort=updated_at_asc&limit=25',
    )
    const params = recordSearchParamsFromFilters(filters)
    expect(params.toString()).toBe(
      'q=disk&type=migration&tag=ops&subject=vps%3A%3Acontext%3A&sort=updated_at_asc&limit=25',
    )
    expect(recordSearchFiltersFromSearchParams(params)).toEqual(filters)
    expect(recordSearchParamsFromFilters(DEFAULT_RECORD_SEARCH_FILTERS).toString()).toBe('')
  })

  it('gives equal filters one key regardless of how they were written', () => {
    expect(recordSearchFilterKey(parse('q=+disk+&type=migration')))
      .toBe(recordSearchFilterKey(parse('type=migration&q=disk')))
    expect(recordSearchFilterKey(parse('q=disk')))
      .not.toBe(recordSearchFilterKey(parse('q=network')))
  })

  it('names every active filter as a chip, including ones with no control', () => {
    const chips = recordSearchFilterChips(parse(
      'type=troubleshooting&status=resolved&status_group=completed&lifecycle=archived'
      + '&owner=usr_a&participant=usr_b&tag=ops&subject=vps%3Avps_alpha%3Aaffected%3A'
      + '&follow_up=overdue&action=open&updated_from=2026-08-02T00%3A00%3A00Z',
    ))
    expect(chips.map((chip) => chip.label)).toEqual([
      '类型: 排障',
      '状态: 已解决',
      '状态分组: 已完成',
      '生命周期: 已归档',
      '负责人: usr_a',
      '参与人: usr_b',
      '标签: ops',
      '对象: VPS / vps_alpha / 受影响 / 任意位置',
      '跟进: 已逾期',
      '待办: 有待办',
      '更新于之后: 2026-08-02T00:00:00Z',
    ])
  })

  it('removes exactly the chip value, keeping its siblings', () => {
    const chips = recordSearchFilterChips(parse('tag=ops&tag=disk&type=migration'))
    const removed = chips.find((chip) => chip.label === '标签: ops')
    expect(removed?.next).toEqual({ tag: ['disk'], type: ['migration'] })
    expect(recordSearchFilterChips(parse('tag=ops'))[0]?.next).toEqual(DEFAULT_RECORD_SEARCH_FILTERS)
  })

  it('reads a free-text list from one field and writes it back', () => {
    expect(parseFilterValueList(' ops, disk ，io  net ')).toEqual(['ops', 'disk', 'io', 'net'])
    expect(parseFilterValueList('  ,  ')).toBeUndefined()
    expect(formatFilterValueList(['ops', 'disk'])).toBe('ops, disk')
    expect(formatFilterValueList(undefined)).toBe('')
  })

  it('sends only the filters the caller set, and never the cursor from the URL', () => {
    // The cursor is page state, not filter state: keeping it in a shareable URL
    // would pin the link to a generation that has probably been republished.
    const query = recordSearchToAPIQuery(parse('q=disk&type=migration&cursor=leaked'), 'live-cursor')
    expect(query).toEqual({ q: 'disk', type: ['migration'], cursor: 'live-cursor' })
    expect(recordSearchToAPIQuery(DEFAULT_RECORD_SEARCH_FILTERS)).toEqual({})
  })
})
