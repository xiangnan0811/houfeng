import { describe, expect, it } from 'vitest'

import {
  applyAssetDecisionInvalidation,
  INITIAL_ASSET_DECISION_REVISIONS,
} from './invalidation'

describe('asset decision invalidation', () => {
  it('increments every compatibility read domain after a renewal decision', () => {
    const current = { ...INITIAL_ASSET_DECISION_REVISIONS }

    const next = applyAssetDecisionInvalidation(current, {
      type: 'renewal-decision-saved',
      vpsID: 'vps_001',
    })

    expect(next).toEqual({
      portfolio: 1,
      groups: 1,
      manualGroups: 1,
      templates: 1,
      records: 1,
      renewalQueue: 1,
    })
    expect(current).toEqual(INITIAL_ASSET_DECISION_REVISIONS)
    expect(next).not.toBe(current)
  })

  it('increments from existing revisions without resetting any domain', () => {
    const next = applyAssetDecisionInvalidation({
      portfolio: 2,
      groups: 4,
      manualGroups: 6,
      templates: 8,
      records: 10,
      renewalQueue: 12,
    }, {
      type: 'renewal-decision-saved',
      vpsID: 'vps_002',
    })

    expect(next).toEqual({
      portfolio: 3,
      groups: 5,
      manualGroups: 7,
      templates: 9,
      records: 11,
      renewalQueue: 13,
    })
  })
})
