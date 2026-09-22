import { describe, expect, it } from 'vitest'

import {
  clearMonitoringFilters,
  parseMonitoringFilters,
  parseMonitoringQuickView,
  parseMonitoringSelectedIds,
  patchSearchParams,
  resolveMonitoringListHref,
  writeMonitoringQuickView,
  writeMonitoringSelectedIds,
} from './monitoringListUrl'

describe('monitoring list URL codec', () => {
  it('maps dashboard onboarding=pending and abnormal=1 into quick views', () => {
    expect(parseMonitoringQuickView(new URLSearchParams('onboarding=pending'))).toBe('onboarding')
    expect(parseMonitoringQuickView(new URLSearchParams('abnormal=1'))).toBe('abnormal')
    expect(parseMonitoringQuickView(new URLSearchParams('view=runtime-attention'))).toBe('runtime-attention')
  })

  it('round-trips unlimited repeated selections with valid deduplication', () => {
    const current = new URLSearchParams(
      'return_vps=vps_001&selected=mi_001&selected=mi_001&selected=not-an-id&selected=mi_002,mi_003',
    )
    expect(parseMonitoringSelectedIds(current)).toEqual(['mi_001', 'mi_002', 'mi_003'])

    writeMonitoringSelectedIds(current, ['mi_003', 'mi_003', 'mi_004', 'invalid'])
    expect(current.get('return_vps')).toBe('vps_001')
    expect(current.getAll('selected')).toEqual(['mi_003', 'mi_004'])
  })

  it('clears filters without dropping unrelated params including stale scope', () => {
    const current = new URLSearchParams('scope=all&q=tokyo&view=abnormal&return_vps=vps_001&region=ap')
    clearMonitoringFilters(current)
    expect(current.get('scope')).toBe('all')
    expect(current.get('q')).toBe('tokyo')
    expect(current.get('view')).toBe('abnormal')
    expect(current.get('return_vps')).toBe('vps_001')
    expect(current.get('region')).toBeNull()
  })

  it('preserves unrelated params when writing a quick view', () => {
    const next = patchSearchParams(new URLSearchParams('from=dashboard&q=edge&scope=archived'), { extra: '1' })
    writeMonitoringQuickView(next, 'binding-conflict')
    expect(next.get('from')).toBe('dashboard')
    expect(next.get('q')).toBe('edge')
    expect(next.get('extra')).toBe('1')
    expect(next.get('scope')).toBe('archived')
    expect(next.get('view')).toBe('binding-conflict')
  })

  it('accepts only /monitoring list hrefs for return context', () => {
    expect(resolveMonitoringListHref({ monitoringListHref: '/monitoring?view=abnormal&q=tokyo' })).toBe(
      '/monitoring?view=abnormal&q=tokyo',
    )
    expect(resolveMonitoringListHref({ monitoringListHref: '/monitoring/mi_001' })).toBe('/monitoring')
    expect(resolveMonitoringListHref({ monitoringListHref: '/vps?view=unlinked' })).toBe('/monitoring')
    expect(resolveMonitoringListHref({ vpsInventoryHref: '/vps' })).toBe('/monitoring')
    expect(parseMonitoringFilters(new URLSearchParams('health=告警')).health).toBe('告警')
  })
})
