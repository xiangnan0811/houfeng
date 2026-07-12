import { describe, expect, it } from 'vitest'

import type { CommandAuditAction, CommandAuditEvent } from './types'

type ForbiddenCommandAuditKeys<T> = Extract<keyof T, 'stdout' | 'stderr' | 'details'>
type HasForbiddenCommandAuditKeys<T> = ForbiddenCommandAuditKeys<T> extends never ? false : true

describe('command audit Web contract', () => {
  it('defines metadata-only response types without output or details fields', () => {
    const eventHasForbiddenKeys: HasForbiddenCommandAuditKeys<CommandAuditEvent> = false
    const actionHasForbiddenKeys: HasForbiddenCommandAuditKeys<CommandAuditAction> = false
    expect(eventHasForbiddenKeys).toBe(false)
    expect(actionHasForbiddenKeys).toBe(false)
  })
})
