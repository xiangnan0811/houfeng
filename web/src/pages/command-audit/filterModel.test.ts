import { describe, expect, it } from 'vitest'

import {
  DEFAULT_COMMAND_AUDIT_FILTERS,
  commandAuditFilterKey,
  commandAuditFiltersFromSearchParams,
  commandAuditSearchParamsFromFilters,
  commandAuditToAPIQuery,
} from './filterModel'

describe('command audit filter model', () => {
  it('omits the default 30 day window from the URL and API query', () => {
    const filters = commandAuditFiltersFromSearchParams(new URLSearchParams())
    expect(filters).toEqual(DEFAULT_COMMAND_AUDIT_FILTERS)
    expect(commandAuditSearchParamsFromFilters(filters).toString()).toBe('')
    expect(commandAuditToAPIQuery(filters)).toEqual({})
  })

  it('normalizes and serializes non-default primary and advanced filters', () => {
    const filters = commandAuditFiltersFromSearchParams(new URLSearchParams(
      'window=7d&monitoring_instance=%20Tokyo%20&command_id=uptime&outcome=failed&sensitivity=sensitive&actor=%20admin%20&action_id=%20act_001%20',
    ))

    expect(filters).toMatchObject({
      window: '7d',
      monitoring_instance: 'Tokyo',
      command_id: 'uptime',
      outcome: 'failed',
      sensitivity: 'sensitive',
      actor: 'admin',
      action_id: 'act_001',
    })
    expect(commandAuditSearchParamsFromFilters(filters).toString()).toBe(
      'window=7d&monitoring_instance=Tokyo&command_id=uptime&outcome=failed&sensitivity=sensitive&actor=admin&action_id=act_001',
    )
    expect(commandAuditToAPIQuery(filters)).toEqual({
      window: '7d',
      monitoring_instance: 'Tokyo',
      command_id: 'uptime',
      outcome: 'failed',
      sensitivity: 'sensitive',
      actor: 'admin',
      action_id: 'act_001',
    })
  })

  it('keeps valid custom bounds and converts them to RFC3339 for the API', () => {
    const filters = commandAuditFiltersFromSearchParams(new URLSearchParams(
      'window=custom&started_from=2026-07-01T08%3A00&started_to=2026-07-02T08%3A00',
    ))

    expect(filters.window).toBe('custom')
    expect(commandAuditSearchParamsFromFilters(filters).toString()).toBe(
      'window=custom&started_from=2026-07-01T08%3A00&started_to=2026-07-02T08%3A00',
    )
    expect(commandAuditToAPIQuery(filters)).toEqual({
      window: 'custom',
      started_from: new Date('2026-07-01T08:00').toISOString(),
      started_to: new Date('2026-07-02T08:00').toISOString(),
    })
  })

  it('canonicalizes invalid values and incomplete custom windows to defaults', () => {
    const filters = commandAuditFiltersFromSearchParams(new URLSearchParams(
      'window=custom&started_from=bad&outcome=done&command_id=rm_rf&sensitivity=private&unknown=value',
    ))

    expect(filters).toEqual(DEFAULT_COMMAND_AUDIT_FILTERS)
    expect(commandAuditSearchParamsFromFilters(filters).toString()).toBe('')
  })

  it('rejects impossible calendar dates instead of letting Date.parse roll them forward', () => {
    const filters = commandAuditFiltersFromSearchParams(new URLSearchParams(
      'window=custom&started_from=2026-02-30T08%3A00&started_to=2026-03-03T08%3A00',
    ))

    expect(filters).toEqual(DEFAULT_COMMAND_AUDIT_FILTERS)
    expect(commandAuditSearchParamsFromFilters(filters).toString()).toBe('')
    expect(commandAuditToAPIQuery(filters)).toEqual({})
  })

  it('uses stable canonical ordering for filter request keys', () => {
    const left = commandAuditFiltersFromSearchParams(new URLSearchParams(
      'actor=admin&window=24h&monitoring_instance=mi_001&outcome=queued',
    ))
    const right = commandAuditFiltersFromSearchParams(new URLSearchParams(
      'outcome=queued&monitoring_instance=mi_001&window=24h&actor=admin',
    ))

    expect(commandAuditFilterKey(left)).toBe(commandAuditFilterKey(right))
    expect(commandAuditFilterKey(left)).toBe(
      'window=24h&monitoring_instance=mi_001&outcome=queued&actor=admin',
    )
  })
})
