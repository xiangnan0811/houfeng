import { matchRoutes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import * as commandAuditPageModule from '../pages/CommandAuditPage'
import { appRoutes } from './router'
import { RequireAuth } from './RequireAuth'

describe('appRoutes error recovery', () => {
  it('provides an error element for protected route failures', () => {
    const matches = matchRoutes(appRoutes, '/events')

    expect(matches).not.toBeNull()
    expect(matches?.some(({ route }) => route.errorElement != null)).toBe(true)
  })
})

describe('command audit route', () => {
  it('keeps the lazy page module named-only like the other routes', () => {
    expect('default' in commandAuditPageModule).toBe(false)
  })

  it('is registered below the private route boundary', () => {
    const matches = matchRoutes(appRoutes, '/command-audit')

    expect(matches).not.toBeNull()
    expect(matches?.some(({ route }) => route.path === 'command-audit')).toBe(true)
    expect(matches?.some(({ route }) => {
      const element = route.element as { type?: unknown } | undefined
      return element?.type === RequireAuth
    })).toBe(true)
  })
})

describe('record inbox route', () => {
  it('is registered below the private route boundary', () => {
    const matches = matchRoutes(appRoutes, '/record-inbox')

    expect(matches).not.toBeNull()
    expect(matches?.some(({ route }) => route.path === 'record-inbox')).toBe(true)
    expect(matches?.some(({ route }) => {
      const element = route.element as { type?: unknown } | undefined
      return element?.type === RequireAuth
    })).toBe(true)
  })
})
