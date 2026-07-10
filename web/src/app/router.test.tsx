import { matchRoutes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { appRoutes } from './router'

describe('appRoutes error recovery', () => {
  it('provides an error element for protected route failures', () => {
    const matches = matchRoutes(appRoutes, '/events')

    expect(matches).not.toBeNull()
    expect(matches?.some(({ route }) => route.errorElement != null)).toBe(true)
  })
})
