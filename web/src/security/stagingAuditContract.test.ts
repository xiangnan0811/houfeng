import { describe, expect, it } from 'vitest'

import { shouldIgnoreResourceConsoleError } from '../../e2e/staging/audit'

const RESOURCE_ERROR_401 =
  'Failed to load resource: the server responded with a status of 401 ()'

describe('staging audit console and HTTP correlation', () => {
  it('ignores a locationless Chromium console error for an explicitly allowed status', () => {
    expect(shouldIgnoreResourceConsoleError(
      new Set(['/api/auth/me 401']),
      RESOURCE_ERROR_401,
      '',
    )).toBe(true)
  })

  it('keeps exact path matching when Chromium provides a console location', () => {
    const allowed = new Set(['/api/auth/me 401'])

    expect(shouldIgnoreResourceConsoleError(
      allowed,
      RESOURCE_ERROR_401,
      'https://staging.example/api/auth/me',
    )).toBe(true)
    expect(shouldIgnoreResourceConsoleError(
      allowed,
      RESOURCE_ERROR_401,
      'https://staging.example/api/private-data',
    )).toBe(false)
  })

  it('does not ignore a locationless status that was not explicitly allowed', () => {
    expect(shouldIgnoreResourceConsoleError(
      new Set(['/api/vps 503']),
      RESOURCE_ERROR_401,
      '',
    )).toBe(false)
  })

  it('does not turn arbitrary console errors into allowed resource failures', () => {
    expect(shouldIgnoreResourceConsoleError(
      new Set(['/api/auth/me 401']),
      'Failed to load resource: net::ERR_CONNECTION_RESET',
      '',
    )).toBe(false)
  })
})
