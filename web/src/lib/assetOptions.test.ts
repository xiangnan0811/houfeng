import { describe, expect, it } from 'vitest'

import {
  legacyFlagsFromRenewalMode,
  normalizeRenewalMode,
  renewalModeFromLegacy,
  renewalModeLabel,
} from './assetOptions'

describe('asset renewal mode options', () => {
  it('keeps lottery and gift renewal modes distinct', () => {
    expect(normalizeRenewalMode('lottery')).toBe('lottery')
    expect(normalizeRenewalMode('gift')).toBe('gift')
    expect(renewalModeLabel('lottery')).toBe('抽奖')
    expect(renewalModeLabel('gift')).toBe('赠送')
  })

  it('treats gift as a non-auto legacy mode', () => {
    expect(renewalModeFromLegacy({ renewal_mode: 'gift', auto_renew: true, auto_renew_cancelled: true })).toBe('gift')
    expect(legacyFlagsFromRenewalMode('gift')).toEqual({ auto_renew: false, auto_renew_cancelled: false })
  })
})
