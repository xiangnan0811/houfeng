import { describe, expect, it } from 'vitest'

import {
  legacyFlagsFromRenewalMode,
  normalizeRenewalMode,
  renewalModeFromLegacy,
  renewalModeLabel,
} from './assetOptions'

describe('asset renewal mode options', () => {
  it('keeps acquisition sources out of renewal-mode choices', () => {
    expect(normalizeRenewalMode('lottery')).toBe('manual')
    expect(normalizeRenewalMode('gift')).toBe('manual')
    expect(renewalModeLabel('manual')).toBe('手动续费')
    expect(normalizeRenewalMode('auto_cancelled')).toBe('auto_cancelled')
  })

  it('uses explicit renewal mode independently from acquisition and stale flags', () => {
    expect(renewalModeFromLegacy({ renewal_mode: 'manual', auto_renew: true, auto_renew_cancelled: true })).toBe('manual')
    expect(legacyFlagsFromRenewalMode('manual')).toEqual({ auto_renew: false, auto_renew_cancelled: false })
  })
})
