import { beforeEach, describe, expect, it } from 'vitest'

import {
  clearOnboardingTokenCache,
  getOnboardingTokenCache,
  setOnboardingTokenCache,
} from './onboardingTokenCache'

describe('onboardingTokenCache', () => {
  beforeEach(() => {
    window.sessionStorage.clear()
  })

  it('stores and reads token data per node in session storage', () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_001',
      issued_at: '2026-04-26T09:10:00Z',
    })

    expect(getOnboardingTokenCache('nd_001')).toEqual({
      token: 'enroll_001',
      issued_at: '2026-04-26T09:10:00Z',
    })
  })

  it('clears cached token data for one node without touching others', () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_001',
      issued_at: '2026-04-26T09:10:00Z',
    })
    setOnboardingTokenCache('nd_002', {
      token: 'enroll_002',
      issued_at: '2026-04-26T09:11:00Z',
    })

    clearOnboardingTokenCache('nd_001')

    expect(getOnboardingTokenCache('nd_001')).toBeNull()
    expect(getOnboardingTokenCache('nd_002')).toEqual({
      token: 'enroll_002',
      issued_at: '2026-04-26T09:11:00Z',
    })
  })

  it('clears malformed or invalid cached entries and returns null', () => {
    const cases = [
      'not json',
      JSON.stringify({ token: '', issued_at: '2026-04-26T09:10:00Z' }),
      JSON.stringify({ token: 'enroll_001', issued_at: '' }),
    ]

    for (const [index, raw] of cases.entries()) {
      const nodeId = `nd_invalid_${index}`
      window.sessionStorage.setItem(`houfeng:onboarding-token:${nodeId}`, raw)

      expect(getOnboardingTokenCache(nodeId)).toBeNull()
      expect(window.sessionStorage.getItem(`houfeng:onboarding-token:${nodeId}`)).toBeNull()
    }
  })
})
